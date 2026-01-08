package gen

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/internal"
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
	if file.Delete {
		_ = os.Remove(file.Path) // Ignore error, file may already be deleted
		return nil
	}

	// Create the file's directory if it doesn't exist. Check if the file
	// itself is a directory. Error if it is.
	{
		dir := path.Dir(file.Path)
		err := os.MkdirAll(dir, 0750)
		if err != nil {
			return microerror.Mask(err)
		}

		f, err := os.Stat(file.Path)
		if os.IsNotExist(err) {
			// Fall through.
		} else if err != nil {
			return microerror.Mask(err)
		} else if f.IsDir() {
			return microerror.Maskf(filePathError, "file %#q is a directory", file.Path)
		}
	}

	if !file.SkipRegenCheck && !isRegenerable(file.Path) {
		return nil
	}

	var permissions fs.FileMode = 0644
	if file.Permissions != 0 {
		permissions = file.Permissions
	}

	w, err := os.OpenFile(file.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, permissions)
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = w.Close() }()

	err = internal.Execute(ctx, w, file)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// isRegenerable returns true if the file should be overridden with the
// regenerated content. All files with "zz_generated." prefix qualify for that
// but there are also some exceptions usually when the name is conventional.
// Files within directories that have the "zz_generated." prefix are also
// considered regenerable (e.g., files in .cursor/rules/zz_generated.* folders).
func isRegenerable(path string) bool {
	base := filepath.Base(path)

	// Check if the file is in a zz_generated.* directory
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		dirBase := filepath.Base(dir)
		if strings.HasPrefix(dirBase, internal.RegenerableFilePrefix) {
			return true
		}
	}

	switch {
	case base == "Makefile" || strings.HasPrefix(base, "Makefile.gen."):
		return true
	case base == ".gitignore":
		return true
	case base == "renovate.json" || base == "renovate.json5":
		return true
	case base == "dependabot.yml":
		return true
	case base == "aws-ami.yaml.template":
		return true
	case strings.HasPrefix(base, internal.RegenerableFilePrefix):
		return true
	}

	return false
}
