package followcircleci

import "github.com/giantswarm/microerror"

var invalidArgError = &microerror.Error{
	Kind: "invalidArgError",
}

// IsInvalidArg asserts invalidArgError: a wrong call.
func IsInvalidArg(err error) bool {
	return microerror.Cause(err) == invalidArgError
}

var redError = &microerror.Error{
	Kind: "redError",
}

var timeoutError = &microerror.Error{
	Kind: "timeoutError",
}

var pendingFailedError = &microerror.Error{
	Kind: "pendingFailedError",
}

// IsPendingFailed asserts pendingFailedError: the set-up of a pending
// repository failed, and the next pass tries again.
func IsPendingFailed(err error) bool {
	return microerror.Cause(err) == pendingFailedError
}
