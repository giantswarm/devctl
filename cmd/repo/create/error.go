package create

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

var refusedError = &microerror.Error{
	Kind: "refusedError",
}

// IsRefused asserts refusedError: the declaration was refused, the problems
// name the fields, and nothing was created or opened.
func IsRefused(err error) bool {
	return microerror.Cause(err) == refusedError
}

var stepFailedError = &microerror.Error{
	Kind: "stepFailedError",
}

// IsStepFailed asserts stepFailedError: the create or the scaffold step
// could not run to its end; a rerun resumes where it stopped.
func IsStepFailed(err error) bool {
	return microerror.Cause(err) == stepFailedError
}

var branchWithoutPullRequestError = &microerror.Error{
	Kind: "branchWithoutPullRequestError",
}

// IsBranchWithoutPullRequest asserts branchWithoutPullRequestError: the
// declaration's branch exists in the team-file repository but no pull
// request is open for it.
func IsBranchWithoutPullRequest(err error) bool {
	return microerror.Cause(err) == branchWithoutPullRequestError
}
