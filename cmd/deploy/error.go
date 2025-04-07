package deploy

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

var invalidArgError = &microerror.Error{
	Kind: "invalidArgError",
}

var envVarNotFoundError = &microerror.Error{
	Kind: "envVarNotFoundError",
}
