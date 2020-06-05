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

func Execute(ctx context.Context, files ...input.Input) error {
	for _, f := range files {
		err := execute(ctx, f)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func execute(ctx context.Context, file input.Input) error {
	// Check if the file's directory exists. Error if it doesn't. If it does check if the
	// file itself is a directory. Error if it is.
	{
		dir := path.Dir(file.Path)
		f, err := os.Stat(dir)
		if os.IsNotExist(err) {
			return microerror.Maskf(filePathError, "directory %#q for file %#q does not exist", dir, path.Base(file.Path))
		} else if err != nil {
			return microerror.Mask(err)
		}

		if !f.IsDir() {
			return microerror.Maskf(filePathError, "file %#q is not a directory", dir)
		}
	}

	if !isRegenerable(file.Path) {
		return nil
	}

	w, err := os.OpenFile(file.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
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

// isRegenerable returns true if the file should be overridden with the
// regenerated content. All files with "zz_generated." prefix qualify for that
// but there are also some exceptions usually when the name is conventional.
func isRegenerable(path string) bool {
	base := filepath.Base(path)

	if base == "Makefile" {
		return true
	}

	if strings.HasPrefix(base, internal.RegenerableFilePrefix) {
		return true
	}

	return false
}
