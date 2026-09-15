package release

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

// pushError indicates that `git push` failed once Release had already
// committed. The commit stays: re-running the command would refuse with a
// not-reserved error, so the fix is `git push` by hand in --repo-dir.
var pushError = &microerror.Error{
	Kind: "pushError",
}

// IsPush asserts pushError.
func IsPush(err error) bool {
	return microerror.Cause(err) == pushError
}
