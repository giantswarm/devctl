package changelog

import (
	"github.com/giantswarm/microerror"
)

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}

var releasedEntriesError = &microerror.Error{
	Kind: "releasedEntriesError",
}

// IsReleasedEntries asserts releasedEntriesError: Unreleased holds entries a
// release already carried.
func IsReleasedEntries(err error) bool {
	return microerror.Cause(err) == releasedEntriesError
}
