package prmerge

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var executionError = &microerror.Error{
	Kind: "executionError",
}

// IsExecution asserts executionError: GitHub did not do what it accepted to
// do within the bound (an update-branch that produced no new head).
func IsExecution(err error) bool {
	return microerror.Cause(err) == executionError
}
