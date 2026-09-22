package client

import "github.com/giantswarm/microerror"

// InvalidFlagError is a flag or argument the command cannot use.
var InvalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts InvalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == InvalidFlagError
}

// UnexpectedAnswerError is an answer of the manager devctl cannot read.
var UnexpectedAnswerError = &microerror.Error{
	Kind: "unexpectedAnswerError",
}

// IsUnexpectedAnswer asserts UnexpectedAnswerError.
func IsUnexpectedAnswer(err error) bool {
	return microerror.Cause(err) == UnexpectedAnswerError
}
