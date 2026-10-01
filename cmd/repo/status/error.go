package status

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var notDeclaredError = &microerror.Error{
	Kind: "notDeclaredError",
}

// IsNotDeclared asserts notDeclaredError: no team file declares the
// repository.
func IsNotDeclared(err error) bool {
	return microerror.Cause(err) == notDeclaredError
}

var noSetupStateError = &microerror.Error{
	Kind: "noSetupStateError",
}

// IsNoSetupState asserts noSetupStateError: the inventory has no checks for
// the repository.
func IsNoSetupState(err error) bool {
	return microerror.Cause(err) == noSetupStateError
}
