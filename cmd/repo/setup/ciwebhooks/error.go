package ciwebhooks

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var invalidArgError = &microerror.Error{
	Kind: "invalidArgError",
}

// IsInvalidArg asserts invalidArgError.
func IsInvalidArg(err error) bool {
	return microerror.Cause(err) == invalidArgError
}

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}

var envVarNotFoundError = &microerror.Error{
	Kind: "envVarNotFoundError",
}

// IsEnvVarNotFound asserts envVarNotFoundError.
func IsEnvVarNotFound(err error) bool {
	return microerror.Cause(err) == envVarNotFoundError
}
