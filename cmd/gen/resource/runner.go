package resource

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/resource"
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
	c := resource.Config{
		Dir:           r.flag.Dir,
		ObjectGroup:   r.flag.ObjectGroup,
		ObjectKind:    r.flag.ObjectKind,
		ObjectVersion: r.flag.ObjectVersion,
	}

	resourceFile, err := resource.NewResource(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		resourceFile,
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
