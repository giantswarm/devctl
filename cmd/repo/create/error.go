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
// name the fields, and no pull request was opened.
func IsRefused(err error) bool {
	return microerror.Cause(err) == refusedError
}
