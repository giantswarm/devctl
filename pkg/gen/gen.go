package gen

import (
	"context"
	"fmt"
	"html/template"
	"os"
	"path"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
)

func Execute(ctx context.Context, files ...input.File) error {
	for _, f := range files {
		in, err := f.GetInput(ctx)
		if err != nil {
			return microerror.Mask(err)
		}

		// Check if the file's directory exists.
		{
			dir := path.Dir(in.Path)
			f, err := os.Stat(dir)
			if os.IsNotExist(err) {
				return microerror.Maskf(filePathError, "directory %#q for file %#q does not exist", dir, path.Base(in.Path))
			} else if err != nil {
				return microerror.Mask(err)
			}

			if !f.IsDir() {
				return microerror.Maskf(filePathError, "file %#q is not a directory", dir)
			}
		}

		tmpl, err := template.New(fmt.Sprintf("%T", f)).Parse(in.TemplateBody)
		if err != nil {
			return microerror.Mask(err)
		}

		w, err := os.OpenFile(in.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
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
