package circleciclient

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var notFoundError = &microerror.Error{
	Kind: "notFoundError",
}

// IsNotFound asserts notFoundError: CircleCI answered 404 — for a project,
// the repository is not followed.
func IsNotFound(err error) bool {
	return microerror.Cause(err) == notFoundError
}

var apiError = &microerror.Error{
	Kind: "apiError",
}

// IsAPI asserts apiError: CircleCI answered with an error status.
func IsAPI(err error) bool {
	return microerror.Cause(err) == apiError
}
