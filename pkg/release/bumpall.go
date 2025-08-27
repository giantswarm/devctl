package release

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/blang/semver"
	tm "github.com/buger/goterm"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v4/api/v1alpha1"
	"github.com/google/go-github/v74/github"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
	"golang.org/x/exp/slices"
)

type componentVersion struct {
	Version       string
	UserRequested bool
}

type appVersion struct {
	Version         string
	UpstreamVersion string
	UserRequested   bool
	DependsOn       []string
}

// BumpAll takes all apps and components in the `input` release and looks up on github for the latest version of each.
// If the version is not specified in the `manuallyRequestedComponents` or `manuallyRequestedApps` it will be bumped to the latest version.
func BumpAll(input v1alpha1.Release, manuallyRequestedComponents []string, manuallyRequestedApps []string, appsToDrop map[string]bool) ([]string, []string, error) {
	requestedComponents := map[string]componentVersion{}
	requestedApps := map[string]appVersion{}

	apps := make(map[string]appVersion)
	components := make(map[string]componentVersion)

	// components
	{
		// Prepare the list of components that the user requested to bump in a more useful way.
		for _, comp := range manuallyRequestedComponents {
			splitted := strings.Split(comp, "@")
			if len(splitted) != 2 {
				return nil, nil, microerror.Maskf(badFormatError, "Error parsing component %q", comp)
			}

			requestedComponents[splitted[0]] = componentVersion{
				Version: splitted[1],
			}
		}

		// Iterate over all components in the input release and bump them if a version was not manually requested by user.
		for _, comp := range input.Spec.Components {
			v := componentVersion{}
			if req, found := requestedComponents[comp.Name]; found {
				// User requested specific version.
				v.Version = req.Version
				v.UserRequested = true
			} else {
				version, err := findNewestComponent(comp.Name)
				if err != nil {
					return nil, nil, microerror.Mask(err)
				}

				v.Version = version.Version
				v.UserRequested = false
			}
			if v.Version != comp.Version {
				components[comp.Name] = v
			}
		}

		// Add any manually requested components that don't exist in the base release
		for name, req := range requestedComponents {
			// Check if this component already exists in the base release
			found := false
			for _, comp := range input.Spec.Components {
				if comp.Name == name {
					found = true
					break
				}
			}
			if !found {
				// This is a new component not in the base release
				components[name] = componentVersion{
					Version:       req.Version,
					UserRequested: true,
				}
			}
		}
	}

	// apps
	{
		// Prepare the list of apps that the user requested to bump in a more useful way.
		for _, app := range manuallyRequestedApps {
			splitted := strings.Split(app, "@")
			if len(splitted) < 2 || len(splitted) > 4 {
				return nil, nil, microerror.Maskf(badFormatError, "Error parsing app %q. Expected format: <name>@<version>[@<component_version>][@<dependencies>]", app)
			}

			req := appVersion{
				Version: splitted[1],
			}

			if len(splitted) > 2 {
				req.UpstreamVersion = splitted[2]
			}

			if len(splitted) > 3 && splitted[3] != "" {
				req.DependsOn = strings.Split(splitted[3], ",")
			}

			requestedApps[splitted[0]] = req
		}

		// Iterate over all apps in the input release and bump them if a version was not manually requested by user.
		for _, app := range input.Spec.Apps {
			v := appVersion{}
			if req, found := requestedApps[app.Name]; found {
				// User requested specific version.
				v.Version = req.Version
				v.UpstreamVersion = req.UpstreamVersion
				v.UserRequested = true
				v.DependsOn = req.DependsOn
			} else {
				version, err := findNewestApp(app.Name, app.ComponentVersion != "")
				if err != nil {
					return nil, nil, microerror.Mask(err)
				}

				v.Version = version.Version
				v.UpstreamVersion = version.UpstreamVersion
				v.UserRequested = false
				v.DependsOn = app.DependsOn
			}
			if v.Version != app.Version || !slices.Equal(v.DependsOn, app.DependsOn) {
				apps[app.Name] = v
			}
		}

		// Add any manually requested apps that don't exist in the base release
		for name, req := range requestedApps {
			// Check if this app already exists in the base release
			found := false
			for _, app := range input.Spec.Apps {
				if app.Name == name {
					found = true
					break
				}
			}
			if !found {
				// This is a new app not in the base release
				apps[name] = appVersion{
					Version:         req.Version,
					UpstreamVersion: req.UpstreamVersion,
					UserRequested:   true,
					DependsOn:       req.DependsOn,
				}
			}
		}
	}

	// Show a recap table with all the updates being applied.
	err := printTable(input, components, apps, appsToDrop)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}

	var char rune
	for string(char) != "y" && string(char) != "Y" && string(char) != "n" && string(char) != "N" {
		fmt.Print("Do you want to continue? (y/n)")
		reader := bufio.NewReader(os.Stdin)
		char, _, err = reader.ReadRune()
		if err != nil {
			return nil, nil, microerror.Mask(err)
		}
	}

	if string(char) == "n" || string(char) == "N" {
		return nil, nil, nil
	}

	fmt.Println("Generating release")

	// Prepare list of components and apps to bump.
	componentsRet := make([]string, 0)
	appsRet := make([]string, 0)

	for name, comp := range components {
		componentsRet = append(componentsRet, fmt.Sprintf("%s@%s", name, comp.Version))
	}
	for name, app := range apps {
		upstreamVersion := app.UpstreamVersion
		dependencies := ""
		if len(app.DependsOn) > 0 {
			dependencies = strings.Join(app.DependsOn, ",")
		}

		if dependencies != "" {
			appsRet = append(appsRet, fmt.Sprintf("%s@%s@%s@%s", name, app.Version, upstreamVersion, dependencies))
		} else if upstreamVersion != "" {
			appsRet = append(appsRet, fmt.Sprintf("%s@%s@%s", name, app.Version, upstreamVersion))
		} else {
			appsRet = append(appsRet, fmt.Sprintf("%s@%s", name, app.Version))
		}
	}

	return componentsRet, appsRet, nil
}

// Just print a table with a list of apps and components with old and new version for easy checking by user.
func printTable(input v1alpha1.Release, components map[string]componentVersion, apps map[string]appVersion, appsToDrop map[string]bool) error {
	tm.Clear()

	t := table.NewWriter()
	t.SetOutputMirror(tm.Output)
	t.AppendHeader(table.Row{"APP NAME", "CURRENT APP VERSION", "DESIRED APP VERSION", "DEPENDENCIES"})
	t.AppendSeparator()
	for _, app := range input.Spec.Apps {
		version := app.Version
		if app.ComponentVersion != "" {
			version = fmt.Sprintf("%s (upstream version %s)", app.Version, app.ComponentVersion)
		}

		var desiredVersion interface{} = "Unchanged"
		var dependencies interface{} = strings.Join(app.DependsOn, ", ")

		if _, dropped := appsToDrop[app.Name]; dropped {
			desiredVersion = "Removed"
			// Color row red
			t.AppendRow(table.Row{
				text.FgRed.Sprint(app.Name),
				text.FgRed.Sprint(version),
				text.FgRed.Sprint(desiredVersion),
				text.FgRed.Sprint(dependencies),
			})
			continue
		}

		if req, found := apps[app.Name]; found {
			desiredVersionStr := req.Version
			if req.UpstreamVersion != "" {
				desiredVersionStr = fmt.Sprintf("%s (upstream version %s)", req.Version, req.UpstreamVersion)
			}
			if req.UserRequested {
				desiredVersionStr = fmt.Sprintf("%s - requested by user", desiredVersionStr)
			}
			desiredVersion = text.FgGreen.Sprint(desiredVersionStr)
			dependencies = text.FgGreen.Sprint(strings.Join(req.DependsOn, ", "))
		}
		t.AppendRow(table.Row{app.Name, version, desiredVersion, dependencies})
	}

	// Add new apps that don't exist in the base release
	for name, req := range apps {
		found := false
		for _, app := range input.Spec.Apps {
			if app.Name == name {
				found = true
				break
			}
		}
		if !found {
			desiredVersionStr := req.Version
			if req.UpstreamVersion != "" {
				desiredVersionStr = fmt.Sprintf("%s (upstream version %s)", req.Version, req.UpstreamVersion)
			}
			if req.UserRequested {
				desiredVersionStr = fmt.Sprintf("%s - requested by user", desiredVersionStr)
			}
			desiredVersion := text.FgGreen.Sprint(desiredVersionStr)
			dependencies := text.FgGreen.Sprint(strings.Join(req.DependsOn, ", "))
			t.AppendRow(table.Row{name, "New app", desiredVersion, dependencies})
		}
	}
	t.AppendSeparator()
	t.Render()

	t = table.NewWriter()
	t.SetOutputMirror(tm.Output)
	t.AppendHeader(table.Row{"COMPONENT NAME", "CURRENT VERSION", "DESIRED VERSION"})
	t.AppendSeparator()
	for _, component := range input.Spec.Components {
		var desiredVersion interface{} = "Unchanged"
		if req, found := components[component.Name]; found {
			desiredVersionStr := req.Version
			if req.UserRequested {
				desiredVersionStr = fmt.Sprintf("%s - requested by user", desiredVersionStr)
			}
			desiredVersion = text.FgGreen.Sprint(desiredVersionStr)
		}
		t.AppendRow(table.Row{component.Name, component.Version, desiredVersion})
	}

	// Add new components that don't exist in the base release
	for name, req := range components {
		found := false
		for _, component := range input.Spec.Components {
			if component.Name == name {
				found = true
				break
			}
		}
		if !found {
			desiredVersionStr := req.Version
			if req.UserRequested {
				desiredVersionStr = fmt.Sprintf("%s - requested by user", desiredVersionStr)
			}
			desiredVersion := text.FgGreen.Sprint(desiredVersionStr)
			t.AppendRow(table.Row{name, "New component", desiredVersion})
		}
	}
	t.AppendSeparator()
	t.Render()

	tm.Flush()
	return nil
}

func findNewestApp(name string, getUpstreamVersion bool) (appVersion, error) {
	var err error
	version := ""

	switch name {
	case "cloud-provider-aws":
		version, err = getLatestGithubRelease("giantswarm", "aws-cloud-controller-manager")
		if err != nil {
			return appVersion{}, microerror.Mask(err)
		}
	case "etcd-k8s-res-count-exporter":
		version, err = getLatestGithubRelease("giantswarm", "etcd-kubernetes-resources-count-exporter")
		if err != nil {
			return appVersion{}, microerror.Mask(err)
		}

	default:
		version, err = getLatestGithubRelease("giantswarm", name)
		if err != nil {
			return appVersion{}, microerror.Mask(err)
		}
	}

	ret := appVersion{
		Version: version,
	}

	if getUpstreamVersion {
		uv, err := getAppVersionFromHelmChart(name, version)
		if err != nil {
			return appVersion{}, microerror.Mask(err)
		}
		ret.UpstreamVersion = uv
	}

	return ret, nil
}

func findNewestComponent(name string) (componentVersion, error) {
	var err error
	version := ""

	switch name {
	case "flatcar":
		version, err = getLatestFlatcarRelease()
		if err != nil {
			return componentVersion{}, microerror.Mask(err)
		}
	case "kubernetes":
		version, err = getLatestGithubRelease("kubernetes", "kubernetes")
		// strip the "Kubernetes " prefix from the version
		version, _ = strings.CutPrefix(version, "Kubernetes ")
		if err != nil {
			return componentVersion{}, microerror.Mask(err)
		}
	case "os-tooling":
		version, err = getLatestGithubRelease("giantswarm", "capi-image-builder")
		if err != nil {
			return componentVersion{}, microerror.Mask(err)
		}
	default:
		version, err = getLatestGithubRelease("giantswarm", name)
		if err != nil {
			return componentVersion{}, microerror.Mask(err)
		}
	}

	version = strings.TrimPrefix(version, "v")

	return componentVersion{
		Version: version,
	}, nil
}

func getLatestGithubRelease(owner string, name string) (string, error) {
	token := os.Getenv("OPSCTL_GITHUB_TOKEN")

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	// Makes sure both `my-fancy-controller` and `my-fancy-controller-app` are getting looked up as `my-fancy-controller-app` and `my-fancy-controller`.
	var candidateNames []string
	if strings.HasSuffix(name, "-app") {
		candidateNames = []string{name, strings.TrimSuffix(name, "-app")}
	} else {
		candidateNames = []string{fmt.Sprintf("%s-app", name), name}
	}

	version := ""
	var latestErr error

	for _, n := range candidateNames {
		release, _, err := client.Repositories.GetLatestRelease(context.Background(), owner, n)
		if IsGithubNotFound(err) {
			// try with next candidate
			latestErr = err
			continue
		} else if err != nil {
			return "", microerror.Mask(err)
		}

		version = *release.Name
		break
	}

	if version == "" {
		return "", microerror.Mask(latestErr)
	}

	version = strings.TrimPrefix(version, "v")

	return version, nil
}

// getKubernetesVersionForMinor fetches the latest patch version for a given Kubernetes minor version
// e.g., for minorVersion "1.31", it might return "1.31.9"
func getKubernetesVersionForMinor(minorVersion string) (string, error) {
	token := os.Getenv("OPSCTL_GITHUB_TOKEN")

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	// Get all releases from kubernetes repository
	opt := &github.ListOptions{
		PerPage: 100, // Get more releases to ensure we find the latest patch
	}

	var latestVersion string
	var latestSemver semver.Version

	for {
		releases, resp, err := client.Repositories.ListReleases(ctx, "kubernetes", "kubernetes", opt)
		if err != nil {
			return "", microerror.Mask(err)
		}

		for _, release := range releases {
			if release.Name == nil {
				continue
			}

			// Handle the "Kubernetes v1.x.y" format from kubernetes/kubernetes releases
			versionStr := *release.Name
			// Strip "Kubernetes " prefix if present
			versionStr = strings.TrimPrefix(versionStr, "Kubernetes ")
			// Strip "v" prefix if present
			versionStr = strings.TrimPrefix(versionStr, "v")

			// Skip pre-releases and versions that don't start with the desired minor version
			if strings.Contains(versionStr, "-") || !strings.HasPrefix(versionStr, minorVersion+".") {
				continue
			}

			version, err := semver.ParseTolerant(versionStr)
			if err != nil {
				continue
			}

			// Check if this version matches our target minor version and is newer than what we found
			if fmt.Sprintf("%d.%d", version.Major, version.Minor) == minorVersion {
				if latestVersion == "" || version.GT(latestSemver) {
					latestSemver = version
					latestVersion = versionStr
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	if latestVersion == "" {
		return "", microerror.Mask(fmt.Errorf("no stable release found for Kubernetes minor version %s", minorVersion))
	}

	return latestVersion, nil
}

// extractKubernetesMinorFromReleaseName attempts to extract a Kubernetes minor version
// from a release name pattern. For example:
// - "v31.0.0" -> "1.31" (mapping v31 to k8s 1.31)
// - "v30.0.0" -> "1.30" (mapping v30 to k8s 1.30)
// Returns the minor version string (like "1.31") or empty string if pattern doesn't match
func extractKubernetesMinorFromReleaseName(releaseName string) string {
	releaseName = strings.TrimPrefix(releaseName, "v")

	// Parse the release version using semver
	releaseVersion, err := semver.ParseTolerant(releaseName)
	if err != nil {
		return ""
	}

	// The pattern maps the major version of the release to a Kubernetes minor version
	// For example: release v31 → k8s 1.31, release v30 → k8s 1.30
	releaseMajor := releaseVersion.Major

	// Convert to kubernetes version pattern: release major = k8s minor
	kubernetesMinor := fmt.Sprintf("1.%d", releaseMajor)

	return kubernetesMinor
}

// autoDetectKubernetesVersion attempts to automatically detect and fetch the appropriate
// Kubernetes version based on the release name pattern
func autoDetectKubernetesVersion(releaseName string) (string, error) {
	kubernetesMinor := extractKubernetesMinorFromReleaseName(releaseName)
	if kubernetesMinor == "" {
		return "", microerror.Mask(fmt.Errorf("could not extract Kubernetes minor version from release name: %s", releaseName))
	}

	latestKubernetesVersion, err := getKubernetesVersionForMinor(kubernetesMinor)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return latestKubernetesVersion, nil
}

func getLatestFlatcarRelease() (string, error) {
	url := "https://www.flatcar.org/releases-json/releases-stable.json"

	var myClient = &http.Client{Timeout: 10 * time.Second}

	r, err := myClient.Get(url)
	if err != nil {
		return "", microerror.Mask(err)
	}
	defer func() { _ = r.Body.Close() }()

	type release struct {
		Channel string
	}

	target := make(map[string]release)

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return "", microerror.Mask(err)
	}

	err = json.Unmarshal(data, &target)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var latest semver.Version
	for name, rel := range target {
		if rel.Channel == "stable" {
			ver, err := semver.ParseTolerant(name)
			if err != nil {
				continue
			}

			if ver.GT(latest) {
				latest = ver
			}
		}
	}

	return latest.String(), nil
}

func getAppVersionFromHelmChart(name string, ref string) (string, error) {
	c := githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: os.Getenv("OPSCTL_GITHUB_TOKEN"),
	}

	client, err := githubclient.New(c)
	if err != nil {
		return "", microerror.Mask(err)
	}

	type repopath struct {
		repo string
		path string
	}

	namewithsuffix := ""
	namewithoutsuffix := ""

	if strings.HasSuffix(name, "-app") {
		namewithsuffix = name
		namewithoutsuffix = strings.TrimSuffix(name, "-app")
	} else {
		namewithoutsuffix = name
		namewithsuffix = fmt.Sprintf("%s-app", name)
	}

	candidates := []repopath{
		{
			repo: namewithsuffix,
			path: fmt.Sprintf("/helm/%s/Chart.yaml", namewithsuffix),
		},
		{
			repo: namewithsuffix,
			path: fmt.Sprintf("/helm/%s/Chart.yaml", namewithoutsuffix),
		},
		{
			repo: namewithoutsuffix,
			path: fmt.Sprintf("/helm/%s/Chart.yaml", namewithsuffix),
		},
		{
			repo: namewithoutsuffix,
			path: fmt.Sprintf("/helm/%s/Chart.yaml", namewithoutsuffix),
		},
	}

	if !strings.HasPrefix(ref, "v") {
		ref = fmt.Sprintf("v%s", ref)
	}

	var data []byte
	for _, candidate := range candidates {
		file, err := client.GetFile(context.Background(), "giantswarm", candidate.repo, candidate.path, ref)
		if err != nil {
			continue
		}

		data = file.Data
	}

	if len(data) == 0 {
		return "", microerror.Maskf(fileNotFoundError, "File /helm/%s/Chart.yaml not found in %s/%s at revision %s", name, "giantswarm", name, ref)
	}

	type chart struct {
		AppVersion string
	}

	crt := chart{}
	err = yaml.Unmarshal(data, &crt)
	if err != nil {
		return "", microerror.Mask(err)
	}

	ret := crt.AppVersion
	ret = strings.TrimPrefix(ret, "v")

	return ret, nil
}
