package release

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v4/api/v1alpha1"
	"github.com/google/go-github/v75/github"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"sigs.k8s.io/yaml"

	"golang.org/x/exp/slices"

	"github.com/giantswarm/devctl/v7/internal/env"
	"github.com/giantswarm/devctl/v7/pkg/githubclient"
	"github.com/giantswarm/devctl/v7/pkg/release/changelog"
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
func BumpAll(input v1alpha1.Release, manuallyRequestedComponents []string, manuallyRequestedApps []string, releaseType string, appsToDrop map[string]bool, requests []Request, yes bool, output string, changesOnly bool, requestedOnly bool, k8sMajorVersion uint64) ([]string, []string, error) {
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
				var err error
				version := componentVersion{}

				if comp.Name == "kubernetes" {
					if releaseType == "patch" {
						// For a patch release, we don't want to automatically bump anything.
						// The user must manually request a bump for a component.
						version.Version = comp.Version
					} else { // major or minor
						version.Version, err = getLatestK8sVersion(k8sMajorVersion)
					}
				} else if comp.Name == "flatcar" {
					if releaseType == "patch" {
						version.Version = comp.Version
					} else { // minor or major
						version.Version, err = getLatestFlatcarRelease()
					}
				} else {
					var latestVersionString string
					latestVersionString, err = findNewestComponentVersion(comp.Name)
					if err == nil {
						if releaseType == "patch" {
							// For a patch release, we don't want to automatically bump anything.
							// The user must manually request a bump for a component.
							version.Version = comp.Version
						} else { // major or minor, no restrictions for other components
							version.Version = latestVersionString
						}
					}
				}

				if err != nil {
					return nil, nil, microerror.Mask(err)
				}

				v.Version = version.Version
				v.UserRequested = false
			}
			if v.Version != comp.Version {
				// We have a new version, let's check against requests.
				var constraint semver.Range
				for _, r := range requests {
					if r.Name == comp.Name {
						c, err := semver.ParseRange(r.Version)
						if err != nil {
							// Ignore invalid constraints.
							continue
						}
						constraint = c
						break
					}
				}

				if constraint != nil {
					newV, err := semver.Parse(v.Version)
					if err != nil {
						return nil, nil, microerror.Mask(err)
					}
					if !constraint(newV) {
						// Does not meet constraint, reverting to old version.
						v.Version = comp.Version
					}
				}

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

			if len(splitted) > 3 {
				if splitted[3] != "" {
					req.DependsOn = strings.Split(splitted[3], ",")
				} else {
					req.DependsOn = []string{}
				}
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
				if req.DependsOn != nil {
					v.DependsOn = req.DependsOn
				} else {
					v.DependsOn = app.DependsOn
				}
			} else {
				if releaseType == "patch" {
					v.Version = app.Version
					v.UpstreamVersion = app.ComponentVersion
					v.UserRequested = false
					v.DependsOn = app.DependsOn
				} else { // major or minor
					version, err := findNewestApp(app.Name, app.ComponentVersion != "")
					if err != nil {
						return nil, nil, microerror.Mask(err)
					}
					v.Version = version.Version
					v.UpstreamVersion = version.UpstreamVersion
					v.UserRequested = false
					v.DependsOn = app.DependsOn
				}
			}
			if v.Version != app.Version || !slices.Equal(v.DependsOn, app.DependsOn) {
				// We have a new version, let's check against requests.
				var constraint semver.Range
				for _, r := range requests {
					if r.Name == app.Name {
						c, err := semver.ParseRange(r.Version)
						if err != nil {
							// Ignore invalid constraints.
							continue
						}
						constraint = c
						break
					}
				}

				if constraint != nil {
					newV, err := semver.Parse(v.Version)
					if err != nil {
						return nil, nil, microerror.Mask(err)
					}
					if !constraint(newV) {
						// Does not meet constraint, reverting to old version.
						v.Version = app.Version
						v.UpstreamVersion = app.ComponentVersion
					}
				}

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
	err := printTable(input, components, apps, appsToDrop, output, changesOnly, requestedOnly)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}

	if !yes {
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
	}

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
			dependencies = strings.Join(app.DependsOn, "#")
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
func printTable(input v1alpha1.Release, components map[string]componentVersion, apps map[string]appVersion, appsToDrop map[string]bool, output string, changesOnly bool, requestedOnly bool) error {
	// --- APPS TABLE ---
	var appRows []table.Row
	for _, app := range input.Spec.Apps {
		req, isUpdated := apps[app.Name]
		_, isDropped := appsToDrop[app.Name]
		isChanged := isUpdated || isDropped

		if requestedOnly {
			if !isUpdated || !req.UserRequested {
				continue
			}
		} else if changesOnly {
			if !isChanged {
				continue
			}
		}

		version := app.Version
		if app.ComponentVersion != "" {
			version = fmt.Sprintf("%s (upstream version %s)", app.Version, app.ComponentVersion)
		}

		var desiredVersion interface{} = "Unchanged"
		var dependencies interface{} = strings.Join(app.DependsOn, ", ")

		if _, dropped := appsToDrop[app.Name]; dropped {
			desiredVersion = "Removed"
		}

		if req, found := apps[app.Name]; found {
			desiredVersionStr := req.Version
			if req.UpstreamVersion != "" {
				desiredVersionStr = fmt.Sprintf("%s (upstream version %s)", req.Version, req.UpstreamVersion)
			}
			if req.UserRequested {
				desiredVersionStr = fmt.Sprintf("%s - requested by user", desiredVersionStr)
			}

			if req.Version != app.Version {
				if output == "text" {
					desiredVersion = text.FgGreen.Sprint(desiredVersionStr)
				} else {
					desiredVersion = fmt.Sprintf("**%s**", desiredVersionStr)
				}
			} else {
				desiredVersion = desiredVersionStr
			}

			if !slices.Equal(req.DependsOn, app.DependsOn) {
				oldDeps := make(map[string]struct{})
				for _, d := range app.DependsOn {
					oldDeps[d] = struct{}{}
				}
				newDeps := make(map[string]struct{})
				for _, d := range req.DependsOn {
					newDeps[d] = struct{}{}
				}

				var unchanged, added, removed []string

				// Find unchanged and removed
				for _, dep := range app.DependsOn {
					if _, ok := newDeps[dep]; ok {
						unchanged = append(unchanged, dep)
					} else {
						if output == "text" {
							removed = append(removed, text.FgRed.Sprintf("~~%s~~", dep))
						} else {
							removed = append(removed, fmt.Sprintf("~~%s~~", dep))
						}
					}
				}

				// Find added
				for _, dep := range req.DependsOn {
					if _, ok := oldDeps[dep]; !ok {
						if output == "text" {
							added = append(added, text.FgGreen.Sprintf("**%s**", dep))
						} else {
							added = append(added, fmt.Sprintf("**%s**", dep))
						}
					}
				}

				sort.Strings(unchanged)
				sort.Strings(added)
				sort.Strings(removed)

				allDeps := append(unchanged, added...)
				allDeps = append(allDeps, removed...)
				dependencies = strings.Join(allDeps, ", ")
			} else {
				dependencies = strings.Join(req.DependsOn, ", ")
			}
		}
		appRows = append(appRows, table.Row{app.Name, version, desiredVersion, dependencies})
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
			var desiredVersion, dependencies string
			if output == "text" {
				desiredVersion = text.FgGreen.Sprint(desiredVersionStr)
				dependencies = text.FgGreen.Sprint(strings.Join(req.DependsOn, ", "))
			} else {
				desiredVersion = fmt.Sprintf("**%s**", desiredVersionStr)
				dependencies = fmt.Sprintf("**%s**", strings.Join(req.DependsOn, ", "))
			}
			appRows = append(appRows, table.Row{name, "New app", desiredVersion, dependencies})
		}
	}

	if len(appRows) > 0 {
		t := table.NewWriter()
		t.SetStyle(table.StyleDefault)
		t.AppendHeader(table.Row{"APP NAME", "CURRENT APP VERSION", "DESIRED APP VERSION", "DEPENDENCIES"})
		t.AppendSeparator()
		t.AppendRows(appRows)
		t.AppendSeparator()
		switch output {
		case "markdown":
			fmt.Println(t.RenderMarkdown())
		default:
			t.SetOutputMirror(os.Stdout)
			t.Render()
		}
	}

	fmt.Println() // Add a blank line between tables

	// --- COMPONENTS TABLE ---
	var componentRows []table.Row
	for _, component := range input.Spec.Components {
		req, isUpdated := components[component.Name]

		if requestedOnly {
			if !isUpdated || !req.UserRequested {
				continue
			}
		} else if changesOnly {
			if !isUpdated {
				continue
			}
		}

		var desiredVersion interface{} = "Unchanged"
		if req, found := components[component.Name]; found {
			desiredVersionStr := req.Version
			if req.UserRequested {
				desiredVersionStr = fmt.Sprintf("%s - requested by user", desiredVersionStr)
			}
			if output == "text" {
				desiredVersion = text.FgGreen.Sprint(desiredVersionStr)
			} else {
				desiredVersion = fmt.Sprintf("**%s**", desiredVersionStr)
			}
		}
		componentRows = append(componentRows, table.Row{component.Name, component.Version, desiredVersion})
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
			var desiredVersion string
			if output == "text" {
				desiredVersion = text.FgGreen.Sprint(desiredVersionStr)
			} else {
				desiredVersion = fmt.Sprintf("**%s**", desiredVersionStr)
			}
			componentRows = append(componentRows, table.Row{name, "New component", desiredVersion})
		}
	}

	if len(componentRows) > 0 {
		t := table.NewWriter()
		t.SetStyle(table.StyleDefault)
		t.AppendHeader(table.Row{"COMPONENT NAME", "CURRENT VERSION", "DESIRED VERSION"})
		t.AppendSeparator()
		t.AppendRows(componentRows)
		t.AppendSeparator()
		switch output {
		case "markdown":
			fmt.Println(t.RenderMarkdown())
		default:
			t.SetOutputMirror(os.Stdout)
			t.Render()
		}
	}

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

func findNewestComponentVersion(name string) (string, error) {
	var err error
	version := ""

	switch name {
	case "flatcar":
		version, err = getLatestFlatcarRelease()
		if err != nil {
			return "", microerror.Mask(err)
		}
	case "kubernetes":
		version, err = getLatestGithubRelease("kubernetes", "kubernetes")
		// strip the "Kubernetes " prefix from the version
		version, _ = strings.CutPrefix(version, "Kubernetes ")
		if err != nil {
			return "", microerror.Mask(err)
		}
	case "os-tooling":
		version, err = getLatestGithubRelease("giantswarm", "capi-image-builder")
		if err != nil {
			return "", microerror.Mask(err)
		}
	default:
		version, err = getLatestGithubRelease("giantswarm", name)
		if err != nil {
			return "", microerror.Mask(err)
		}
	}

	version = strings.TrimPrefix(version, "v")

	return version, nil
}

func findNewestComponent(name string) (componentVersion, error) {
	version, err := findNewestComponentVersion(name)
	if err != nil {
		return componentVersion{}, microerror.Mask(err)
	}

	return componentVersion{
		Version: version,
	}, nil
}

func getLatestGithubRelease(owner string, name string) (string, error) {
	token := env.GitHubToken.Val()

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	owner, candidateNames := getRepoCandidates(owner, name)

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

// getLatestK8sVersion returns the latest patch version for a given k8s major.minor version.
func getLatestK8sVersion(major uint64) (string, error) {
	token := env.GitHubToken.Val()
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	opt := &github.ListOptions{PerPage: 100}
	var allReleases []*github.RepositoryRelease
	for {
		releases, resp, err := client.Repositories.ListReleases(context.Background(), "kubernetes", "kubernetes", opt)
		if err != nil {
			return "", microerror.Mask(err)
		}
		allReleases = append(allReleases, releases...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	var latest semver.Version
	for _, rel := range allReleases {
		if rel.GetPrerelease() {
			continue
		}

		versionName := rel.GetName()
		// Some release names have a "Kubernetes " prefix
		versionName = strings.TrimPrefix(versionName, "Kubernetes ")
		v, err := semver.ParseTolerant(versionName)
		if err != nil {
			continue
		}

		if v.Major == 1 && v.Minor == major {
			if v.GT(latest) {
				latest = v
			}
		}
	}

	if latest.Equals(semver.Version{}) {
		return "", microerror.Maskf(releaseNotFoundError, "no kubernetes release found for major version v1.%d", major)
	}

	return latest.String(), nil
}

// getLatestReleaseForMinor fetches the latest patch version for a given minor version of a component.
// e.g., for minorVersion "1.31", it might return "1.31.9"
func getLatestReleaseForMinor(owner, repo, minorVersion string) (string, error) {
	token := env.GitHubToken.Val()

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	owner, candidateNames := getRepoCandidates(owner, repo)

	var latestVersion string
	var latestSemver semver.Version
	var lastErr error

	for _, repoCandidate := range candidateNames {
		// Get all releases from the repository
		opt := &github.ListOptions{
			PerPage: 100, // Get more releases to ensure we find the latest patch
		}

		var releasesForCandidate []*github.RepositoryRelease
		for {
			releases, resp, err := client.Repositories.ListReleases(ctx, owner, repoCandidate, opt)
			if err != nil {
				// Check for 404 Not Found, and try the next candidate repo.
				if ghErr, ok := err.(*github.ErrorResponse); ok && ghErr.Response.StatusCode == http.StatusNotFound {
					lastErr = err
					goto nextCandidate
				}
				// For other errors, we fail.
				return "", microerror.Mask(err)
			}

			releasesForCandidate = append(releasesForCandidate, releases...)

			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}

		for _, release := range releasesForCandidate {
			if release.Name == nil {
				continue
			}

			versionStr := *release.Name
			// Strip "Kubernetes " prefix if present (for kubernetes/kubernetes)
			versionStr = strings.TrimPrefix(versionStr, "Kubernetes ")
			// Strip "v" prefix if present
			versionStr = strings.TrimPrefix(versionStr, "v")

			// Skip releases of other minors.
			if !strings.HasPrefix(versionStr, minorVersion+".") {
				continue
			}

			version, err := semver.ParseTolerant(versionStr)
			if err != nil {
				continue
			}

			// Check if this version matches our target minor version and is newer than what we found
			currentMinor := fmt.Sprintf("%d.%d", version.Major, version.Minor)
			if currentMinor == minorVersion {
				if latestVersion == "" || version.GT(latestSemver) {
					latestSemver = version
					latestVersion = versionStr
				}
			}
		}

		// If we found a version, we can break from the candidate loop.
		if latestVersion != "" {
			break
		}

	nextCandidate:
	}

	if latestVersion == "" {
		if lastErr != nil {
			return "", microerror.Mask(lastErr)
		}
		return "", microerror.Mask(fmt.Errorf("no stable release found for %s/%s minor version %s", owner, repo, minorVersion))
	}

	return latestVersion, nil
}

func getRepoCandidates(owner, name string) (string, []string) {
	var repoName string
	var newOwner string
	newOwner, repoName = changelog.GetRepoName(name)
	if repoName != "" {
		owner = newOwner
		return owner, []string{repoName}
	}

	// Fallback to old logic if not in map.
	var candidateNames []string
	if strings.HasSuffix(name, "-app") {
		candidateNames = []string{name, strings.TrimSuffix(name, "-app")}
	} else {
		candidateNames = []string{fmt.Sprintf("%s-app", name), name}
	}

	return owner, candidateNames
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

// autoDetectVersion attempts to automatically detect and fetch the appropriate
// version based on the release name pattern
func autoDetectVersion(releaseName, componentName string) (string, error) {
	kubernetesMinor := extractKubernetesMinorFromReleaseName(releaseName)
	if kubernetesMinor == "" {
		return "", microerror.Mask(fmt.Errorf("could not extract Kubernetes minor version from release name: %s", releaseName))
	}

	owner, repoName := changelog.GetRepoName(componentName)
	if repoName == "" {
		return "", microerror.Mask(fmt.Errorf("could not get repository for app %s", componentName))
	}

	latestVersion, err := getLatestReleaseForMinor(owner, repoName, kubernetesMinor)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return latestVersion, nil
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
		AccessToken: env.GitHubToken.Val(),
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
