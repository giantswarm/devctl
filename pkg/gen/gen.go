package gen

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func Execute(ctx context.Context, files ...input.File) error {
	for _, f := range files {
		err := execute(ctx, f)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func execute(ctx context.Context, file input.File) error {
	in, err := file.GetInput(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	// Check if the file's directory exists. Error if it doesn't. If it does check if the
	// file itself is a directory. Error if it is.
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

	// Check if file exist. If it does and it is not prefixed with
	// "zz_generated." return.
	{
		base := filepath.Base(in.Path)
		if !strings.HasPrefix(base, internal.RegenerableFilePrefix) {
			// Skip.
			return nil
		}
	}

	w, err := os.OpenFile(in.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return microerror.Mask(err)
	}
	defer w.Close()

	err = internal.Execute(ctx, w, file)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
