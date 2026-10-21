package githubclient

import "github.com/giantswarm/microerror"

var executionError = &microerror.Error{
	Kind: "executionError",
}

// IsExecution asserts executionError.
func IsExecution(err error) bool {
	return microerror.Cause(err) == executionError
}

var notFoundError = &microerror.Error{
	Kind: "notFoundError",
}

// IsNotFound asserts notFoundError.
func IsNotFound(err error) bool {
	return microerror.Cause(err) == notFoundError
}

// NotFound is the error of a read GitHub answers 404, what IsNotFound
// asserts: for a GitHub of its own, a test double, to answer as the client.
func NotFound(format string, args ...any) error {
	return microerror.Maskf(notFoundError, format, args...)
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var installationNotFoundError = &microerror.Error{
	Kind: "installationNotFoundError",
}

// IsInstallationNotFound asserts installationNotFoundError.
func IsInstallationNotFound(err error) bool {
	return microerror.Cause(err) == installationNotFoundError
}

var prMergeTimeoutError = &microerror.Error{
	Kind: "prMergeTimeoutError",
}

// IsPRMergeTimeout asserts prMergeTimeoutError.
func IsPRMergeTimeout(err error) bool {
	return microerror.Cause(err) == prMergeTimeoutError
}
