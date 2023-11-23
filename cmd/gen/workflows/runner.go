package workflows

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v6/pkg/gen"
	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows"
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

	var workflowsInput *workflows.Workflows
	{
		c := workflows.Config{
			EnableFloatingMajorVersionTags: r.flag.EnableFloatingMajorVersionTags,
			Flavours:                       r.flag.Flavours,
		}

		workflowsInput, err = workflows.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	inputs := []input.Input{
		workflowsInput.CreateRelease(),
		workflowsInput.CreateReleasePR(),
		workflowsInput.FixVulnerabilities(),
	}

	if r.flag.CheckSecrets {
		inputs = append(inputs, workflowsInput.Gitleaks())
	}

	if r.flag.EnableFloatingMajorVersionTags {
		inputs = append(inputs, workflowsInput.EnsureMajorVersionTags())
	}

	if r.flag.Flavours.Contains(gen.FlavourApp) {
		inputs = append(inputs, workflowsInput.CheckValuesSchema())
		if r.flag.InstallUpdateChart {
			inputs = append(inputs, workflowsInput.UpdateChart())
		}
	}

	if r.flag.Flavours.Contains(gen.FlavourCustomer) {
		inputs = append(inputs, workflowsInput.AddCustomerBoardAutomation())
	}

	if r.flag.Flavours.Contains(gen.FlavourClusterApp) {
		inputs = append(inputs, workflowsInput.ClusterAppDocumentationValidation())
		inputs = append(inputs, workflowsInput.ClusterAppSchemaValidation())
		inputs = append(inputs, workflowsInput.HelmRenderDiff())
	}

	err = gen.Execute(
		ctx,
		inputs...,
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
