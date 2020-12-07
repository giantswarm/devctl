package dependabot

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input"
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

	var dependabotInput *dependabot.Dependabot
	{
		c := dependabot.Config{
			Interval:   r.flag.Interval,
			Reviewers:  r.flag.Reviewers,
			Ecosystems: r.flag.Ecosystems,
		}

		dependabotInput, err = dependabot.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var hasGomodEcosystem bool
	for _, e := range r.flag.Ecosystems {
		if e == gen.EcosystemGomod.String() {
			hasGomodEcosystem = true
			break
		}
	}

	var inputs []input.Input
	{
		inputs = append(inputs, dependabotInput.CreateDependabot())

		if hasGomodEcosystem {
			inputs = append(inputs, dependabotInput.CreateWorkflow())
		}
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
