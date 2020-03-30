package internal

import (
	"context"
	"fmt"
	"io"
	"text/template"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/microerror"
)

func Execute(ctx context.Context, w io.Writer, f input.Input) error {
	tmpl, err := template.New(fmt.Sprintf("%T", f)).Parse(f.TemplateBody)
	if err != nil {
		return microerror.Mask(err)
	}
	err = tmpl.Execute(w, f.TemplateData)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
