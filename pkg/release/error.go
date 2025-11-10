package release

import (
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v78/github"
)

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

// Indicates that an item exists as a different type (e.g. trying to add a component that exists as an app).
var invalidItemTypeError = &microerror.Error{
	Kind: "invalidItemTypeError",
}

// IsInvalidItemType asserts invalidItemTypeError.
func IsInvalidItemType(err error) bool {
	return microerror.Cause(err) == invalidItemTypeError
}

func IsGithubNotFound(err error) bool {
	if err == nil {
		return false
	}

	v, ok := err.(*github.ErrorResponse)
	if !ok {
		return false
	}

	return v.Message == "Not Found"
}

var fileNotFoundError = &microerror.Error{
	Kind: "fileNotFoundError",
}
