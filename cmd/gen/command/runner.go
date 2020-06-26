package command

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/command"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg"
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
	var err error

	var commandInput *command.Command
	{
		c := command.Config(*r.flag)

		commandInput, err = command.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var mainInput *mainpkg.Main
	{
		c := mainpkg.Config{
			Name: r.flag.Name,
		}

		mainInput, err = mainpkg.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	err = gen.Execute(
		ctx,
		commandInput.Flags(),
		commandInput.Meta(),
		commandInput.Run(),
		commandInput.ZZCommand(),
		commandInput.ZZError(),
		commandInput.ZZFlags(),
		commandInput.ZZRunner(),
		mainInput.ZZMain(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
