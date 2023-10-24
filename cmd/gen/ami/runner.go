package ami

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v6/pkg/gen"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/ami"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	c := ami.Config(*r.flag)

	amiInput, err := ami.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = amiInput.Boot(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		amiInput.AMIFile(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
