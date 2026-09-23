package deploy

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

var invalidArgError = &microerror.Error{
	Kind: "invalidArgError",
}
