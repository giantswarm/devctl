package dependabot

import (
	"context"
	"io"
	"strings"

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

	var c dependabot.Config
	{
		if len(r.flag.Reviewers) > 0 {
			c.Reviewers = reviewerList(r.flag.Reviewers)
		} else {
			c.Reviewers = nil
		}
	}

	dependabotInput, err := dependabot.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		dependabotInput.CreateDependabot(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func reviewerList(f string) []string {
	return strings.Split(f, ",")
}
