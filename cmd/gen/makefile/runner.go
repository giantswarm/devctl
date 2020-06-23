package makefile

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile"
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
	flavour := mapFlavourTypeToMakeFileFlavour(r.flag.Flavour)
	makefileInput, err := makefile.New(flavour)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		makefileInput.Makefile(),
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func mapFlavourTypeToMakeFileFlavour(f string) int {
	switch f {
	case flavourApp:
		return makefile.FlavourApp

	case flavourCLI:
		return makefile.FlavourCLI

	case flavourOperator:
		return makefile.FlavourOperator

	case flavourLibrary:
		return makefile.FlavourLibrary

	default:
		return makefile.FlavourOperator
	}
}
