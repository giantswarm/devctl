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
	"github.com/google/go-github/v63/github"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v6/pkg/githubclient"
)

type componentVersion struct {
	Version       string
	UserRequested bool
}

type appVersion struct {
	Version         string
	UpstreamVersion string
	UserRequested   bool
}

// BumpAll takes all apps and components in the `input` release and looks up on github for the latest version of each.
// If the version is not specified in the `manuallyRequestedComponents` or `manuallyRequestedApps` it will be bumped to the latest version.
func BumpAll(input v1alpha1.Release, manuallyRequestedComponents []string, manuallyRequestedApps []string) ([]string, []string, error) {
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
	}

	// apps
	{
		// Prepare the list of apps that the user requested to bump in a more useful way.
		for _, app := range manuallyRequestedApps {
			splitted := strings.Split(app, "@")
			if len(splitted) != 2 && len(splitted) != 3 {
				return nil, nil, microerror.Maskf(badFormatError, "Error parsing app %q", app)
			}

			req := appVersion{
				Version: splitted[1],
			}

			if len(splitted) > 2 {
				req.UpstreamVersion = splitted[2]
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
			} else {
				version, err := findNewestApp(app.Name, app.ComponentVersion != "")
				if err != nil {
					return nil, nil, microerror.Mask(err)
				}

				v.Version = version.Version
				v.UpstreamVersion = version.UpstreamVersion
				v.UserRequested = false
			}
			if v.Version != app.Version {
				apps[app.Name] = v
			}
		}
	}

	// Show a recap table with all the updates being applied.
	err := printTable(input, components, apps)
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
		r := fmt.Sprintf("%s@%s", name, app.Version)
		if app.UpstreamVersion != "" {
			r = fmt.Sprintf("%s@%s", r, app.UpstreamVersion)
		}
		appsRet = append(appsRet, r)
	}

	return componentsRet, appsRet, nil
}

// Just print a table with a list of apps and components with old and new version for easy checking by user.
func printTable(input v1alpha1.Release, components map[string]componentVersion, apps map[string]appVersion) error {
	tm.Clear()

	t := table.NewWriter()
	t.SetOutputMirror(tm.Output)
	t.AppendHeader(table.Row{"App Name", "Current App Version", "Desired App Version"})
	t.AppendSeparator()
	for _, app := range input.Spec.Apps {
		version := app.Version
		if app.ComponentVersion != "" {
			version = fmt.Sprintf("%s (upstream version %s)", app.Version, app.ComponentVersion)
		}
		desiredVersion := "Unchanged"
		if req, found := apps[app.Name]; found {
			desiredVersion = req.Version
			if req.UpstreamVersion != "" {
				desiredVersion = fmt.Sprintf("%s (upstream version %s)", req.Version, req.UpstreamVersion)
			}
			if req.UserRequested {
				desiredVersion = fmt.Sprintf("%s - requested by user", desiredVersion)
			}
		}
		t.AppendRow(table.Row{app.Name, version, desiredVersion})
	}
	t.AppendSeparator()
	t.Render()

	t = table.NewWriter()
	t.SetOutputMirror(tm.Output)
	t.AppendHeader(table.Row{"Component Name", "Current Version", "Desired Version"})
	t.AppendSeparator()
	for _, component := range input.Spec.Components {
		desiredVersion := ""
		if req, found := components[component.Name]; found {
			desiredVersion = req.Version
			if req.UserRequested {
				desiredVersion = fmt.Sprintf("%s - requested by user", desiredVersion)
			}
		}
		t.AppendRow(table.Row{component.Name, component.Version, desiredVersion})
	}
	t.AppendSeparator()
	t.Render()

	tm.Flush()
	return nil
}

func findNewestApp(name string, getUpstreamVersion bool) (appVersion, error) {
	version, err := getLatestGithubRelease("giantswarm", name)
	if err != nil {
		return appVersion{}, microerror.Mask(err)
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
	case "containerlinux":
		version, err = getLatestFlatcarRelease()
		if err != nil {
			return componentVersion{}, microerror.Mask(err)
		}
	case "kubernetes":
		version, err = getLatestGithubRelease("kubernetes", "kubernetes")
		if err != nil {
			return componentVersion{}, microerror.Mask(err)
		}
	case "calico":
		version, err = getLatestGithubRelease("projectcalico", "calico")
		if err != nil {
			return componentVersion{}, microerror.Mask(err)
		}
	case "etcd":
		version, err = getLatestGithubRelease("etcd-io", "etcd")
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
	//client := github.NewClient(nil)

	candidateNames := []string{name}
	if strings.HasSuffix(name, "-app") {
		candidateNames = append(candidateNames, strings.TrimSuffix(name, "-app"))
	} else {
		candidateNames = append(candidateNames, fmt.Sprintf("%s-app", name))
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

func getLatestFlatcarRelease() (string, error) {
	url := "https://www.flatcar.org/releases-json/releases-stable.json"

	var myClient = &http.Client{Timeout: 10 * time.Second}

	r, err := myClient.Get(url)
	if err != nil {
		return "", microerror.Mask(err)
	}
	defer r.Body.Close()

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
		DryRun:      false,
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
