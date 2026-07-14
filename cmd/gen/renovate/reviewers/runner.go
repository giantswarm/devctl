package reviewers

import (
	"context"
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/renovate"
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
	path, err := renovate.FindConfigFile(".")
	if err != nil {
		return microerror.Mask(err)
	}

	err = renovate.SetReviewers(path, r.flag.Reviewers)
	if err != nil {
		return microerror.Mask(err)
	}

	fmt.Fprintf(r.stdout, "Set reviewers in %s to %v\n", path, r.flag.Reviewers)

	return nil
}
