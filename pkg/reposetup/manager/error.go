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
// not with MCP.
func IsUnreachable(err error) bool {
	return microerror.Cause(err) == unreachableError
}

var authRequiredError = &microerror.Error{
	Kind: "authRequiredError",
}

// IsAuthRequired asserts authRequiredError: the endpoint refused the bearer
// (401 or 403); the person logs in to muster again.
func IsAuthRequired(err error) bool {
	return microerror.Cause(err) == authRequiredError
}

var toolError = &microerror.Error{
	Kind: "toolError",
}

// IsTool asserts toolError: the manager answered with an error or an
// answer the client does not understand.
func IsTool(err error) bool {
	return microerror.Cause(err) == toolError
}
