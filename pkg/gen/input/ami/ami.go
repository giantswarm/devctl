package ami

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/ami/internal/file"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/ami/internal/params"
)

type AMI struct {
	config Config

	booted bool
	params params.Params
}

func New(config Config) (*AMI, error) {
	err := config.Validate()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	f := &AMI{
		config: config,
	}

	return f, nil
}

func (a *AMI) AMIFile() input.Input {
	a.mustBooted()
	return file.NewAMIInput(a.params)
}

func (a *AMI) Boot(ctx context.Context) error {
	err := a.initParams(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	a.booted = true
	return nil
}

func (a *AMI) initParams(ctx context.Context) error {
	amiInfoString, err := getAMIInfoString(a.config)
	if err != nil {
		return microerror.Mask(err)
	}

	a.params = params.Params{
		AMIInfoString: amiInfoString,
		Dir:           a.config.Dir,
	}

	return nil
}

func (a *AMI) mustBooted() {
	if !a.booted {
		panic(fmt.Sprintf("%T must be booted", a))
	}
}
