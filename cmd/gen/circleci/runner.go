package circleci

import (
	"context"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/circleci"
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

func (r *runner) run(ctx context.Context, _ *cobra.Command, _ []string) error {
	var err error

	// The image pipeline is derived from repo content: architect already
	// requires a Dockerfile to build an image, so its presence is the signal.
	_, statErr := os.Stat("Dockerfile")
	hasDockerfile := statErr == nil

	var circleciInput *circleci.CircleCI
	{
		c := circleci.Config{
			RepoName:      r.flag.RepoName,
			Language:      r.flag.Language,
			Flavours:      r.flag.Flavours,
			HasDockerfile: hasDockerfile,
			OrbVersion:    r.flag.OrbVersion,
		}

		circleciInput, err = circleci.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	inputs := []input.Input{
		circleciInput.Config(),
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
