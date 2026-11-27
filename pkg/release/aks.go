package release

import (
	"context"
	"regexp"
	"strings"

	"github.com/blang/semver"
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// The repository AKS publishes its release notes in.
const (
	aksGitHubOwner = "Azure"
	aksGitHubRepo  = "AKS"
)

var (
	// Matches the release note line that announces new patch versions, e.g.
	// "Kubernetes patch versions `1.36.3`, `1.35.7`, and `1.34.10` are now available."
	aksPatchVersionsLine = regexp.MustCompile(`(?i)kubernetes patch versions?`)
	aksKubernetesVersion = regexp.MustCompile(`\b1\.\d+\.\d+\b`)
)

// parseAKSKubernetesVersions returns the Kubernetes patch versions that AKS
// release notes announce as available.
func parseAKSKubernetesVersions(releaseNotes string) []semver.Version {
	var versions []semver.Version
	for _, line := range strings.Split(releaseNotes, "\n") {
		if !aksPatchVersionsLine.MatchString(line) {
			continue
		}
		for _, match := range aksKubernetesVersion.FindAllString(line, -1) {
			v, err := semver.Parse(match)
			if err != nil {
				continue
			}
			versions = append(versions, v)
		}
	}
	return versions
}

// latestAKSKubernetesVersion returns the highest patch version of Kubernetes
// v1.<minor> among the given versions.
func latestAKSKubernetesVersion(versions []semver.Version, minor uint64) (string, error) {
	if len(versions) == 0 {
		return "", microerror.Maskf(releaseNotFoundError, "found no kubernetes patch versions in the release notes of %s/%s, their format may have changed", aksGitHubOwner, aksGitHubRepo)
	}

	var latest semver.Version
	for _, v := range versions {
		if v.Major == 1 && v.Minor == minor && v.GT(latest) {
			latest = v
		}
	}
	if latest.Equals(semver.Version{}) {
		return "", microerror.Maskf(releaseNotFoundError, "the release notes of %s/%s announce no kubernetes v1.%d patch version, pass one with --component kubernetes@<version>", aksGitHubOwner, aksGitHubRepo, minor)
	}

	return latest.String(), nil
}

// getLatestAKSK8sVersion returns the latest patch version of Kubernetes
// v1.<minor> that AKS offers. AKS lags behind upstream Kubernetes, so the
// upstream tags do not tell which versions a cluster can be created with. The
// AKS release notes do, and need no Azure credentials. A version is announced
// when its rollout starts, which takes a few days to reach every region.
func getLatestAKSK8sVersion(githubToken string, minor uint64) (string, error) {
	client, err := github.NewClient(github.WithAuthToken(githubToken))
	if err != nil {
		return "", microerror.Mask(err)
	}

	// The 100 most recent release notes span several years.
	releases, _, err := client.Repositories.ListReleases(context.Background(), aksGitHubOwner, aksGitHubRepo, &github.ListOptions{PerPage: 100})
	if err != nil {
		return "", microerror.Mask(err)
	}

	var versions []semver.Version
	for _, rel := range releases {
		if rel.GetPrerelease() || rel.GetDraft() {
			continue
		}
		versions = append(versions, parseAKSKubernetesVersions(rel.GetBody())...)
	}

	return latestAKSKubernetesVersion(versions, minor)
}
