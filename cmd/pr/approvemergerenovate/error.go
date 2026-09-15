package approvemergerenovate

import (
	"github.com/giantswarm/microerror"
)

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

var invalidFlagsError = &microerror.Error{
	Kind: "invalidFlagsError",
}

var executionFailedError = &microerror.Error{
	Kind: "executionFailedError",
}

// stateFailure is the GitHub commit-status state and check-run conclusion that
// marks a failed run.
const stateFailure = "failure"
