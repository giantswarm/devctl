package followcircleci

import "github.com/giantswarm/microerror"

var invalidArgError = &microerror.Error{
	Kind: "invalidArgError",
}

var redError = &microerror.Error{
	Kind: "redError",
}

var timeoutError = &microerror.Error{
	Kind: "timeoutError",
}
