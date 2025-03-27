package release

import (
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v70/github"
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
