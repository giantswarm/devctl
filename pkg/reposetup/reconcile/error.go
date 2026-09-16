package reconcile

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError: the run could not start.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}
