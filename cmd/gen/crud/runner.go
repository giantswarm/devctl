package crud

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/crud"
)

type runner struct {
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
	crudInput, err := crud.NewCRUD()
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		crudInput.CRUD(),
		crudInput.Patch(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
