package makefile

import (
	"context"
	"io"
	"os"
	"text/template"

	mtemplate "github.com/giantswarm/devctl/cmd/gen/makefile/template"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
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
	var temp = template.New("Makefile")
	t := template.Must(temp.Parse(mtemplate.Makefile))
	data := mtemplate.TemplateConfig{
		Application: r.flag.App,
	}

	if r.flag.Output == "" {
		err = t.Execute(os.Stdout, data)
		if err != nil {
			return microerror.Mask(err)
		}
	} else {
		file, err := os.Create(r.flag.Output)
		if err != nil {
			return microerror.Mask(err)
		}
		err = t.Execute(file, data)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}
