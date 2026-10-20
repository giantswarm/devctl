package reservation

import (
	"regexp"
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

// githubLogin is a GitHub login: 1-39 alphanumerics or single hyphens, never
// starting or ending with a hyphen, plus the "[bot]" suffix of an app's login.
var githubLogin = regexp.MustCompile(`^[A-Za-z0-9](?:-?[A-Za-z0-9]){0,38}(?:\[bot\])?$`)

// ValidateUser checks the form every --user flag takes. The login is written
// into YAML, so anything else would corrupt the reservation's source object.
func ValidateUser(user string) error {
	if !githubLogin.MatchString(user) {
		return microerror.Maskf(invalidConfigError, "user %q is not a GitHub login", user)
	}

	return nil
}
