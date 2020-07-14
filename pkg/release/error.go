package release

import "github.com/giantswarm/microerror"

// Indicates that the release was not found for the given provider and version.
var releaseNotFoundError = &microerror.Error{
	Kind: "releaseNotFoundError",
}

// IsInvalidConfig asserts releaseNotFoundError.
func IsReleaseNotFound(err error) bool {
	return microerror.Cause(err) == releaseNotFoundError
}

// Indicates that the component or app is incorrectly formatted.
var badFormatError = &microerror.Error{
	Kind: "badFormatError",
}

// IsBadFormat asserts badFormatError.
func IsBadFormat(err error) bool {
	return microerror.Cause(err) == badFormatError
}
