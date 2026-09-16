package reap

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}

// invalidPullRequestError indicates that a reservation's stored pull request
// is not owner/repo#number, so its head branch cannot be looked up.
var invalidPullRequestError = &microerror.Error{
	Kind: "invalidPullRequestError",
}

// IsInvalidPullRequest asserts invalidPullRequestError.
func IsInvalidPullRequest(err error) bool {
	return microerror.Cause(err) == invalidPullRequestError
}

// reapError wraps whatever reservation.Reap reports once it has already
// printed every release it did manage: a broken cluster or a failed push
// among several must not hide the releases that landed.
var reapError = &microerror.Error{
	Kind: "reapError",
}

// IsReap asserts reapError.
func IsReap(err error) bool {
	return microerror.Cause(err) == reapError
}
