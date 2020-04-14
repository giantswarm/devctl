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

var wrongNumberOfVersionsFoundError = &microerror.Error{
	Kind: "wrongNumberOfVersionsFoundError",
}

var NoVersionFoundInFileError = &microerror.Error{
	Kind: "NoVersionFoundInFileError",
}

var NoUnreleasedWorkFoundInChangelogError = &microerror.Error{
	Kind: "NoUnreleasedWorkFoundInChangelogError",
}

var tokenNotFoundError = &microerror.Error{
	Kind: "tokenNotFoundError",
}

var unreachableRepositoryError = &microerror.Error{
	Kind: "unreachableRepositoryError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}
