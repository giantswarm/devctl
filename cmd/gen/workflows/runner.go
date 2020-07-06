package workflows

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows"
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

	var flavour gen.Flavour
	{
		flavour, err = gen.NewFlavour(r.flag.Flavour)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var workflowsInput *workflows.Workflows
	{
		c := workflows.Config{
			Flavour: flavour,
		}

		workflowsInput, err = workflows.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	err = gen.Execute(
		ctx,
		workflowsInput.CreateRelease(),
		workflowsInput.CreateReleasePR(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
