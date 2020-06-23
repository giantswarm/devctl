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
	flavour, err := mapFlavourTypeToMakeFileFlavour(r.flag.Flavour)
	if err != nil {
		return microerror.Mask(err)
	}

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

func mapFlavourTypeToMakeFileFlavour(f string) (int, error) {
	switch f {
	case flavourApp:
		return makefile.FlavourApp, nil

	case flavourCLI:
		return makefile.FlavourCLI, nil

	case flavourOperator:
		return makefile.FlavourOperator, nil

	case flavourLibrary:
		return makefile.FlavourLibrary, nil

	default:
		return 0, microerror.Maskf(invalidFlagError, "the picked flavour is invalid")
	}
}
