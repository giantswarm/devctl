package gen

import "github.com/giantswarm/microerror"

var filePathError = &microerror.Error{
	Kind: "filePathError",
}

// IsFilePath asserts filePathError.
func IsFilePath(err error) bool {
	return microerror.Cause(err) == filePathError
}
