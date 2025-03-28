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

var deploymentTimeoutError = &microerror.Error{
	Kind: "deploymentTimeoutError",
}

var prMergeTimeoutError = &microerror.Error{
	Kind: "prMergeTimeoutError",
}

var kubectlError = &microerror.Error{
	Kind: "kubectlError",
}

var gitError = &microerror.Error{
	Kind: "gitError",
}
