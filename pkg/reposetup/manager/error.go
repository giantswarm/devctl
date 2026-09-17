package manager

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var unreachableError = &microerror.Error{
	Kind: "unreachableError",
}

// IsUnreachable asserts unreachableError: the endpoint did not answer, or
// not with MCP. The caller falls back to the engine.
func IsUnreachable(err error) bool {
	return microerror.Cause(err) == unreachableError
}

var toolError = &microerror.Error{
	Kind: "toolError",
}

// IsTool asserts toolError: the manager answered with an error or an
// answer the client does not understand.
func IsTool(err error) bool {
	return microerror.Cause(err) == toolError
}
