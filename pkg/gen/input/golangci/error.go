package golangci

import "github.com/giantswarm/microerror"

var unsupportedConfigError = &microerror.Error{
	Kind: "unsupportedConfigError",
}

// IsUnsupportedConfig asserts unsupportedConfigError.
func IsUnsupportedConfig(err error) bool {
	return microerror.Cause(err) == unsupportedConfigError
}
