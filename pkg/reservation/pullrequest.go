package reservation

import (
	"strconv"
	"strings"

	"github.com/giantswarm/microerror"
)

// SplitPullRequest parses "owner/repo#number", the form every --pull-request
// flag stores and matches on.
func SplitPullRequest(pullRequest string) (owner, repo string, number int, err error) {
	repoPart, numberPart, ok := strings.Cut(pullRequest, "#")
	if !ok {
		return "", "", 0, microerror.Maskf(invalidConfigError, "pull request %q is not owner/repo#number", pullRequest)
	}
	owner, repo, ok = strings.Cut(repoPart, "/")
	if !ok || owner == "" || repo == "" {
		return "", "", 0, microerror.Maskf(invalidConfigError, "pull request %q is not owner/repo#number", pullRequest)
	}
	number, err = strconv.Atoi(numberPart)
	if err != nil {
		return "", "", 0, microerror.Maskf(invalidConfigError, "pull request %q is not owner/repo#number: %v", pullRequest, err)
	}

	return owner, repo, number, nil
}
