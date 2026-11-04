package makefile

import (
	"context"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/golangci"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/makefile"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}

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

	// A fork line carries its upstream's files: nothing is generated for it.
	if !r.flag.Flavours.Generates() {
		return nil
	}

	var inputs []input.Input
	var obsolete []string

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
		} else {
			obsolete = append(obsolete, "Makefile.gen.app.mk")
		}

		if r.flag.Flavours.Contains(gen.FlavourKubernetesAPI) {
			inputs = append(inputs, in.MakefileGenKubernetesAPI()...)
		} else {
			obsolete = append(obsolete, "Makefile.gen.k8sapi.mk")
		}

		if r.flag.Flavours.Contains(gen.FlavourClusterApp) {
			inputs = append(inputs, in.MakefileGenClusterApp())
		} else {
			obsolete = append(obsolete, "Makefile.gen.cluster_app.mk")
		}

		if r.flag.Language == gen.LanguageGo {
			inputs = append(inputs, in.MakefileGenGo()...)
			inputs = append(inputs, golangci.NewInput("."))
		} else {
			obsolete = append(obsolete, "Makefile.gen.go.mk")
		}

		if r.flag.Language == gen.LanguageKyvernoPolicy {
			inputs = append(inputs, in.MakefileGenChainsaw()...)
		} else {
			obsolete = append(obsolete, "Makefile.gen.chainsaw.mk")
		}
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	// Retire only managed includes, after generating the selected files. Custom
	// includes and repository-owned scaffolding must survive a declaration change.
	for _, path := range obsolete {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return microerror.Mask(err)
		}
	}

	return nil
}
