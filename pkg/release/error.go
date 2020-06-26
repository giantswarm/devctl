package release

import "github.com/giantswarm/microerror"

var releaseNotFoundError = &microerror.Error{
	Kind: "releaseNotFoundError",
}

// IsInvalidConfig asserts releaseNotFoundError.
func IsReleaseNotFound(err error) bool {
	return microerror.Cause(err) == releaseNotFoundError
}
