package internal

import (
	"context"
	"fmt"
	"io"
	"text/template"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
)

func Execute(ctx context.Context, w io.Writer, f input.File) error {
	in, err := f.GetInput(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	tmpl, err := template.New(fmt.Sprintf("%T", f)).Parse(in.TemplateBody)
	if err != nil {
		return microerror.Mask(err)
	}
	err = tmpl.Execute(w, in.TemplateData)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
