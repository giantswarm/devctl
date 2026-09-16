package internal

import "github.com/giantswarm/microerror"

var invalidInputError = &microerror.Error{
	Kind: "invalidInputError",
}

// IsInvalidInput asserts invalidInputError.
func IsInvalidInput(err error) bool {
	return microerror.Cause(err) == invalidInputError
}
