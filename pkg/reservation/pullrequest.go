package reservation

import (
	"regexp"
	"strconv"

	"github.com/giantswarm/microerror"
)

// pullRequestForm is "owner/repo#number": a GitHub owner, a repository name
// that is not "." or "..", and a positive number without leading zeros.
var pullRequestForm = regexp.MustCompile(`^([A-Za-z0-9-]+)/([A-Za-z0-9._-]+)#([1-9][0-9]*)$`)

// SplitPullRequest parses "owner/repo#number", the form every --pull-request
// flag stores and matches on.
func SplitPullRequest(pullRequest string) (owner, repo string, number int, err error) {
	m := pullRequestForm.FindStringSubmatch(pullRequest)
	if m == nil || m[2] == "." || m[2] == ".." {
		return "", "", 0, microerror.Maskf(invalidConfigError, "pull request %q is not owner/repo#number", pullRequest)
	}
	number, err = strconv.Atoi(m[3])
	if err != nil {
		return "", "", 0, microerror.Maskf(invalidConfigError, "pull request %q is not owner/repo#number: %v", pullRequest, err)
	}

	return m[1], m[2], number, nil
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
