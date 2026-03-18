package precommit

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}
