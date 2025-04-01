package appstatus

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

var executionFailedError = &microerror.Error{
	Kind: "executionFailedError",
}
