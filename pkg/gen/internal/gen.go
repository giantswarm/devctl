package internal

import (
	"context"
	"fmt"
	"io"
	"text/template"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

func Execute(ctx context.Context, w io.Writer, f input.Input) error {
	var err error

	if f.Generate != nil {
		if f.TemplateBody != "" {
			return microerror.Maskf(invalidInputError, "%T sets both Generate and TemplateBody, only one of them can produce the file", f)
		}

		content, err := f.Generate(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
		if _, err := w.Write(content); err != nil {
			return microerror.Mask(err)
		}
		return nil
	}

	tmpl := template.New(fmt.Sprintf("%T", f))

	emptyDelims := input.InputTemplateDelims{}
	if f.TemplateDelims != emptyDelims {
		tmpl = tmpl.Delims(f.TemplateDelims.Left, f.TemplateDelims.Right)
	}

	tmpl, err = tmpl.Parse(f.TemplateBody)
	if err != nil {
		return microerror.Mask(err)
	}

	err = tmpl.Execute(w, f.TemplateData)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
