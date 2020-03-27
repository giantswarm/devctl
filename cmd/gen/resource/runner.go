package resource

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/resource"
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

	resourceInput, err := resource.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		resourceInput.CreateFile(),
		resourceInput.CurrentFile(),
		resourceInput.ResourceFile(),
	)
	if gen.IsFilePath(err) {
		fmt.Fprintf(r.stderr, "%s\n", err)
		os.Exit(1)
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
