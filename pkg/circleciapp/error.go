package circleciapp

import "github.com/giantswarm/microerror"

var projectNotFoundError = &microerror.Error{
	Kind: "projectNotFoundError",
}

// IsProjectNotFound asserts projectNotFoundError: CircleCI knows no project
// for the repository.
func IsProjectNotFound(err error) bool {
	return microerror.Cause(err) == projectNotFoundError
}

var noConfigError = &microerror.Error{
	Kind: "noConfigError",
}

// IsNoConfig asserts noConfigError: the repository's default branch carries
// no CircleCI configuration.
func IsNoConfig(err error) bool {
	return microerror.Cause(err) == noConfigError
}
