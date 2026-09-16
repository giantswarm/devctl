package engine

import "github.com/giantswarm/microerror"

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

var stepFailedError = &microerror.Error{
	Kind: "stepFailedError",
}

// IsStepFailed asserts stepFailedError: a step could not run to its end.
func IsStepFailed(err error) bool {
	return microerror.Cause(err) == stepFailedError
}
