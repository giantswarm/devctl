package gen

import (
	"context"
	"fmt"
	"html/template"
	"os"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
)

func Execute(ctx context.Context, files ...input.File) error {
	for _, f := range files {
		in, err := f.GetInput(ctx)
		if err != nil {
			return microerror.Mask(err)
		}

		tmpl, err := template.New(fmt.Sprintf("%T", f)).Parse(in.TemplateBody)
		if err != nil {
			return microerror.Mask(err)
		}

		// TODO override, ignore
		w, err := os.OpenFile(in.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			return microerror.Mask(err)
		}
		defer w.Close()

		err = tmpl.Execute(w, in.TemplateData)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}
