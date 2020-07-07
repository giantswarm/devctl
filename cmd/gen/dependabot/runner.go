package dependabot

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/dependabot"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var err error

	var dependabotInput *dependabot.Dependabot
	{
		c := dependabot.Config{
			Daily:     r.flag.Daily,
			Reviewers: r.flag.Reviewers,
		}

		dependabotInput, err = dependabot.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	err = gen.Execute(
		ctx,
		dependabotInput.CreateDependabot(),
		dependabotInput.CreateWorkflow(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
