package makefile

import (
	"context"
	"io"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
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
	c := makefile.Config(*r.flag)

	makefileInput, err := makefile.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = makefileInput.Params(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		makefileInput.Makefile(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
