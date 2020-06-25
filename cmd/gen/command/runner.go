package command

import (
	"context"
	"io"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/command"
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
	c := command.Config(*r.flag)

	commandInput, err := command.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		commandInput.Command(),
		commandInput.Error(),
		commandInput.Flags(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
