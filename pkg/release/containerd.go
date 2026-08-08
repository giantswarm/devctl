package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/releases/sdk/api/v1alpha1"
	"github.com/google/go-github/v90/github"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/internal/env"
)

const (
	containerdComponentName = "containerd"
	osToolingComponentName  = "os-tooling"

	osToolingOwner = "giantswarm"
	osToolingRepo  = "capi-image-builder"

	// imageBuilderContainerdConfigURL points at the upstream packer config that defines the
	// containerd version installed into /opt/bin on every node.
	imageBuilderContainerdConfigURL = "https://raw.githubusercontent.com/kubernetes-sigs/image-builder/%s/images/capi/packer/config/containerd.json"
)

// imageBuilderVersionRegexp matches the upstream image-builder pin in the capi-image-builder
// Dockerfile, e.g. `ARG IMAGE_BUILDER_VERSION=v0.1.55`.
var imageBuilderVersionRegexp = regexp.MustCompile(`(?m)^ARG\s+IMAGE_BUILDER_VERSION=(\S+)`)

// containerdVersionCache keeps the two lookups below to one round trip per os-tooling version.
var containerdVersionCache = map[string]string{}

// findContainerdVersion resolves the containerd version baked into our node images for a given
// os-tooling (capi-image-builder) version.
//
// Node images are built by giantswarm/capi-image-builder, which pins an upstream
// kubernetes-sigs/image-builder version in its Dockerfile. That upstream release carries the
// containerd version in its packer config, and that is the version nodes actually run — it
// differs from the one Flatcar embeds, which is why it is worth recording in the release.
func findContainerdVersion(osToolingVersion string) (string, error) {
	if cached, ok := containerdVersionCache[osToolingVersion]; ok {
		return cached, nil
	}

	imageBuilderVersion, err := findImageBuilderVersion(osToolingVersion)
	if err != nil {
		return "", microerror.Mask(err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf(imageBuilderContainerdConfigURL, imageBuilderVersion)
	response, err := client.Get(url)
	if err != nil {
		return "", microerror.Mask(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", microerror.Maskf(executionFailedError, "unexpected status %d fetching %s", response.StatusCode, url)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var config struct {
		ContainerdVersion string `json:"containerd_version"`
	}
	err = json.Unmarshal(body, &config)
	if err != nil {
		return "", microerror.Mask(err)
	}

	if config.ContainerdVersion == "" {
		return "", microerror.Maskf(executionFailedError, "no containerd_version found in %s", url)
	}

	containerdVersionCache[osToolingVersion] = config.ContainerdVersion

	return config.ContainerdVersion, nil
}

// findImageBuilderVersion reads the upstream image-builder version pinned by the given
// capi-image-builder release. The repository is private, so this goes through the API rather
// than raw.githubusercontent.com.
func findImageBuilderVersion(osToolingVersion string) (string, error) {
	client, err := github.NewClient(github.WithAuthToken(env.GitHubToken.Val()))
	if err != nil {
		return "", microerror.Mask(err)
	}

	opts := &github.RepositoryContentGetOptions{Ref: fmt.Sprintf("v%s", osToolingVersion)}
	reader, _, err := client.Repositories.DownloadContents(context.Background(), osToolingOwner, osToolingRepo, "Dockerfile", opts)
	if err != nil {
		return "", microerror.Mask(err)
	}
	defer func() { _ = reader.Close() }()

	dockerfile, err := io.ReadAll(reader)
	if err != nil {
		return "", microerror.Mask(err)
	}

	match := imageBuilderVersionRegexp.FindSubmatch(dockerfile)
	if match == nil {
		return "", microerror.Maskf(executionFailedError, "no IMAGE_BUILDER_VERSION found in %s/%s Dockerfile at v%s", osToolingOwner, osToolingRepo, osToolingVersion)
	}

	return string(match[1]), nil
}

// deriveContainerdVersion returns the containerd version implied by the given os-tooling
// version. An empty os-tooling version yields an empty result: providers that do not build
// their own node images (AKS) have no os-tooling component and therefore no containerd entry.
//
// A lookup failure is not fatal. The version is informational, and a release should not fail
// to be created because a tag is missing or GitHub is briefly unavailable.
func deriveContainerdVersion(osToolingVersion string) string {
	if osToolingVersion == "" {
		return ""
	}

	version, err := findContainerdVersion(osToolingVersion)
	if err != nil {
		logrus.Warnf("Could not determine containerd version for os-tooling v%s: %v", osToolingVersion, err)
		return ""
	}

	return version
}

// applyContainerdComponent records the containerd version that comes with the release's
// os-tooling version as a component of its own, so it shows up in the Release CR and the
// release notes.
//
// The version is always derived here and never requested: it is a property of the node image,
// so anything else would record a version no node runs.
func applyContainerdComponent(updates *v1alpha1.Release, base v1alpha1.Release, verbose bool) {
	osToolingVersion := lookupComponentVersion(updates.Spec.Components, osToolingComponentName)
	if osToolingVersion == "" {
		osToolingVersion = lookupComponentVersion(base.Spec.Components, osToolingComponentName)
	}

	containerdVersion := deriveContainerdVersion(osToolingVersion)
	if containerdVersion == "" {
		return
	}

	if verbose {
		fmt.Printf("Derived containerd v%s from os-tooling v%s\n", containerdVersion, osToolingVersion)
	}

	updates.Spec.Components = append(updates.Spec.Components, v1alpha1.ReleaseSpecComponent{
		Name:    containerdComponentName,
		Version: containerdVersion,
	})
}

func lookupComponentVersion(components []v1alpha1.ReleaseSpecComponent, name string) string {
	for _, component := range components {
		if component.Name == name {
			return component.Version
		}
	}

	return ""
}
