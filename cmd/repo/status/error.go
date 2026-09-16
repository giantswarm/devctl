package status

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}

var invalidDeclarationError = &microerror.Error{
	Kind: "invalidDeclarationError",
}

// IsInvalidDeclaration asserts invalidDeclarationError: the team file's
// entry is refused by the engine's validation, so its set-up state cannot
// be judged until the entry is fixed; the problems name the fields.
func IsInvalidDeclaration(err error) bool {
	return microerror.Cause(err) == invalidDeclarationError
}

var notDeclaredError = &microerror.Error{
	Kind: "notDeclaredError",
}

// IsNotDeclared asserts notDeclaredError: no team file declares the
// repository, so there is no desired state to judge it against.
func IsNotDeclared(err error) bool {
	return microerror.Cause(err) == notDeclaredError
}
