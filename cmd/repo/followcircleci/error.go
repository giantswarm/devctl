package followcircleci

import "github.com/giantswarm/microerror"

var invalidArgError = &microerror.Error{
	Kind: "invalidArgError",
}

var notFollowedError = &microerror.Error{
	Kind: "notFollowedError",
}

var webhookMissingError = &microerror.Error{
	Kind: "webhookMissingError",
}
