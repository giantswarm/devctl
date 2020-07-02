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

	var c workflows.Config
	{
		c.Flavour, err = mapFlavourTypeToWorkflowFlavour(r.flag.Flavour)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	workflowsInput, err := workflows.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		workflowsInput.CreateRelease(),
		workflowsInput.CreateReleaseBranch(),
		workflowsInput.CreateReleasePR(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func mapFlavourTypeToWorkflowFlavour(f string) (int, error) {
	switch f {
	case flavourApp:
		return workflows.FlavourApp, nil

	case flavourCLI:
		return workflows.FlavourCLI, nil

	case flavourOperator:
		return workflows.FlavourOperator, nil

	case flavourLibrary:
		return workflows.FlavourLibrary, nil

	default:
		return 0, microerror.Maskf(invalidFlagError, "the picked flavour is invalid")
	}
}
