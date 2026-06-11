package renovate

import "github.com/giantswarm/microerror"

var configNotFoundError = &microerror.Error{
	Kind: "configNotFoundError",
}

// IsConfigNotFound asserts configNotFoundError.
func IsConfigNotFound(err error) bool {
	return microerror.Cause(err) == configNotFoundError
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}
