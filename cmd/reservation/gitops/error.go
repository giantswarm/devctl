package gitops

import "github.com/giantswarm/microerror"

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}

// syncError indicates that the checkout cannot safely be synced to
// origin/main: it is off main, dirty, ahead, diverged or cannot fetch.
var syncError = &microerror.Error{
	Kind: "syncError",
}

// IsSync asserts syncError.
func IsSync(err error) bool {
	return microerror.Cause(err) == syncError
}

// pushFailedError indicates that a reservation change could not be pushed.
var pushFailedError = &microerror.Error{
	Kind: "pushFailedError",
}

// IsPushFailed asserts pushFailedError.
func IsPushFailed(err error) bool {
	return microerror.Cause(err) == pushFailedError
}
