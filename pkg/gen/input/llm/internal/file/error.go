package file

import "github.com/giantswarm/microerror"

// invalidManagedBlockError means the repository's AGENTS.md has devctl-managed
// markers that do not form exactly one block.
var invalidManagedBlockError = &microerror.Error{
	Kind: "invalidManagedBlockError",
}

// IsInvalidManagedBlock asserts invalidManagedBlockError.
func IsInvalidManagedBlock(err error) bool {
	return microerror.Cause(err) == invalidManagedBlockError
}

// invalidBlockBodyError means devctl's own block content contains a marker line.
var invalidBlockBodyError = &microerror.Error{
	Kind: "invalidBlockBodyError",
}
