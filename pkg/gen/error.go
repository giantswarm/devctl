package gen

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var filePathError = &microerror.Error{
	Kind: "filePathError",
}

// IsFilePath asserts filePathError.
func IsFilePath(err error) bool {
	return microerror.Cause(err) == filePathError
}
