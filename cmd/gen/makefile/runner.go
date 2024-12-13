package makefile

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile"
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

	var inputs []input.Input

	// Makefile
	// Makefile.app.mk
	// Makefile.go.mk
	{
		c := makefile.Config{
			Flavours: r.flag.Flavours,
		}

		in, err := makefile.New(c)
		if err != nil {
			return microerror.Mask(err)
		}

		inputs = append(inputs, in.Makefile())

		if r.flag.Flavours.Contains(gen.FlavourApp) {
			inputs = append(inputs, in.MakefileGenApp())
		}

		if r.flag.Flavours.Contains(gen.FlavourKubernetesAPI) {
			inputs = append(inputs, in.MakefileGenKubernetesAPI()...)
		}

		if r.flag.Flavours.Contains(gen.FlavourClusterApp) {
			inputs = append(inputs, in.MakefileGenClusterApp())
		}

		if r.flag.Language == gen.LanguageGo {
			inputs = append(inputs, in.MakefileGenGo()...)
		}
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
