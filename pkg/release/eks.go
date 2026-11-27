package release

import (
	"context"
	"regexp"
	"slices"
	"strconv"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// The repository the Amazon EKS user guide is written in, and the pages that
// have one section per Kubernetes version EKS offers.
const (
	eksDocsGitHubOwner = "awsdocs"
	eksDocsGitHubRepo  = "amazon-eks-user-guide"
)

var eksDocsVersionPages = []string{
	"latest/ug/versioning/kubernetes-versions-standard.adoc",
	"latest/ug/versioning/kubernetes-versions-extended.adoc",
}

// Matches the section heading of a Kubernetes version, e.g. "== Kubernetes 1.37".
var eksVersionHeading = regexp.MustCompile(`(?m)^== Kubernetes 1\.(\d+)\s*$`)

// parseEKSKubernetesMinors returns the Kubernetes minor versions that a page
// of the Amazon EKS user guide has a section for.
func parseEKSKubernetesMinors(page string) []uint64 {
	var minors []uint64
	for _, match := range eksVersionHeading.FindAllStringSubmatch(page, -1) {
		minor, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil {
			continue
		}
		minors = append(minors, minor)
	}
	return minors
}

// checkEKSOffersKubernetesMinor returns an error unless Kubernetes v1.<minor>
// is among the given minor versions.
func checkEKSOffersKubernetesMinor(minors []uint64, minor uint64) error {
	if len(minors) == 0 {
		return microerror.Maskf(releaseNotFoundError, "found no kubernetes versions in the EKS user guide of %s/%s, its format may have changed", eksDocsGitHubOwner, eksDocsGitHubRepo)
	}
	if !slices.Contains(minors, minor) {
		return microerror.Maskf(releaseNotFoundError, "the EKS user guide of %s/%s lists no kubernetes v1.%d, EKS may not offer it yet", eksDocsGitHubOwner, eksDocsGitHubRepo, minor)
	}
	return nil
}

// checkEKSKubernetesMinor returns an error unless EKS offers Kubernetes
// v1.<minor>. AWS runs the EKS control plane and picks the patch version, but
// adds a new minor version some time after upstream releases it. The EKS user
// guide lists the minor versions on standard and extended support, and reading
// it needs no AWS credentials.
func checkEKSKubernetesMinor(githubToken string, minor uint64) error {
	client, err := github.NewClient(github.WithAuthToken(githubToken))
	if err != nil {
		return microerror.Mask(err)
	}

	var minors []uint64
	for _, path := range eksDocsVersionPages {
		file, _, _, err := client.Repositories.GetContents(context.Background(), eksDocsGitHubOwner, eksDocsGitHubRepo, path, nil)
		if err != nil {
			return microerror.Mask(err)
		}
		if file == nil {
			return microerror.Maskf(releaseNotFoundError, "%s in %s/%s is not a file", path, eksDocsGitHubOwner, eksDocsGitHubRepo)
		}
		page, err := file.GetContent()
		if err != nil {
			return microerror.Mask(err)
		}
		minors = append(minors, parseEKSKubernetesMinors(page)...)
	}

	return checkEKSOffersKubernetesMinor(minors, minor)
}
