package makefile

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input"
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
	var err error

	var flavour gen.Flavour
	{
		flavour, err = gen.NewFlavour(r.flag.Flavour)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var language gen.Language
	{
		language, err = gen.NewLanguage(r.flag.Language)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var inputs []input.Input

	// Makefile
	// Makefile.go.mk
	{
		c := makefile.Config{
			Flavour: flavour,
		}

		in, err := makefile.New(c)
		if err != nil {
			return microerror.Mask(err)
		}

		inputs = append(inputs, in.Makefile())

		if language == gen.LanguageGo {
			inputs = append(inputs, in.MakefileGo())
		}
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
