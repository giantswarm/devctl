package gen

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/batchci"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/internal"
)

// Execute processes inputs in order. If an input fails or ctx is canceled,
// earlier successful writes remain; later inputs are not started.
func Execute(ctx context.Context, files ...input.Input) error {
	// A workflow generation always refreshes the aggregate from the same
	// repository-owned policy; alignment never needs to edit generated output.
	for _, f := range files {
		if !f.Delete && strings.Contains(filepath.ToSlash(f.Path), ".github/workflows/") {
			policy, err := batchci.Read(batchci.Root(f.Path))
			if err != nil {
				return microerror.Mask(err)
			}
			if policy != nil {
				files = append(files, input.Input{
					Path:     filepath.Join(batchci.Root(f.Path), ".github/workflows/zz_generated.batch_ci.yaml"),
					Generate: func(context.Context) ([]byte, error) { return batchci.Gate(policy) },
				})
			}
			break
		}
	}

	for _, f := range files {
		err := execute(ctx, f)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func execute(ctx context.Context, file input.Input) error {
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}

	if file.Delete {
		err := os.Remove(file.Path)
		// Kubernetes API cleanup also removes empty legacy tool directories.
		// A nonempty directory can contain tools the repository still owns.
		if err != nil && !os.IsNotExist(err) && !os.IsExist(err) {
			return microerror.Mask(err)
		}
		return nil
	}

	// Create the file's directory if it doesn't exist. Check if the file
	// itself is a directory. Error if it is.
	var fileExists bool
	{
		dir := filepath.Dir(file.Path)
		err := os.MkdirAll(dir, 0750)
		if err != nil {
			return microerror.Mask(err)
		}

		f, err := os.Stat(file.Path)
		fileExists = true
		if os.IsNotExist(err) {
			// Fall through but add a condition for later so the file gets created.
			fileExists = false
		} else if err != nil {
			return microerror.Mask(err)
		} else if f.IsDir() {
			return microerror.Maskf(filePathError, "file %#q is a directory", file.Path)
		}
	}

	if !file.SkipRegenCheck && !isRegenerable(file.Path) && fileExists {
		return nil
	}

	var permissions fs.FileMode = 0644
	if file.Permissions != 0 {
		permissions = file.Permissions
	}

	// Render completely before starting publication. A Generate callback can
	// fail outside devctl (for example while fetching schema references), and
	// previously committed output must survive that failure too.
	// See https://github.com/giantswarm/devctl/issues/2195.
	var buf bytes.Buffer
	if err := internal.Execute(ctx, &buf, file); err != nil {
		return microerror.Mask(err)
	}

	// Apply the persistent repository policy only to generated CI files.
	if strings.Contains(filepath.ToSlash(file.Path), ".github/workflows/") || strings.HasSuffix(filepath.ToSlash(file.Path), ".circleci/config.yml") {
		policy, err := batchci.Read(batchci.Root(file.Path))
		if err != nil {
			return microerror.Mask(err)
		}
		content, err := batchci.Apply(file.Path, buf.Bytes(), policy)
		if err != nil {
			return microerror.Mask(err)
		}
		buf.Reset()
		_, _ = buf.Write(content)
	}

	// A generator may finish with content after its caller has canceled.
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}

	if fileExists && file.TemplateBody != "" {
		existing, err := os.ReadFile(file.Path) // #nosec G304 -- same caller-selected destination as the write below
		if err != nil {
			return microerror.Mask(err)
		}
		if onlyFallbackProvenanceChanged(existing, buf.Bytes()) {
			return nil
		}
	}
	return writeGeneratedFile(file.Path, buf.Bytes(), permissions)
}

// A build without generated .sha files has only a module tree link. Keep the
// existing provenance when that less precise link is the only change. Real
// template commit links and changes anywhere after the header still get written.
func onlyFallbackProvenanceChanged(existing, rendered []byte) bool {
	const prefix = "# DO NOT EDIT. Generated with:\n#\n#    devctl\n#\n#    https://github.com/giantswarm/devctl/"
	existingPrelude, existingHeader, existingOK := bytes.Cut(existing, []byte(prefix))
	renderedPrelude, renderedHeader, renderedOK := bytes.Cut(rendered, []byte(prefix+"tree/"))
	if !existingOK || !renderedOK || !bytes.Equal(existingPrelude, renderedPrelude) {
		return false
	}
	_, existingBody, _ := bytes.Cut(existingHeader, []byte("\n"))
	_, renderedBody, _ := bytes.Cut(renderedHeader, []byte("\n"))
	return bytes.HasPrefix(existingBody, []byte("#\n")) && bytes.Equal(existingBody, renderedBody)
}

// isRegenerable returns true if the file should be overridden with the
// regenerated content. All files with "zz_generated." prefix qualify for that
// but there are also some exceptions usually when the name is conventional.
func isRegenerable(path string) bool {
	base := filepath.Base(path)
	path = filepath.ToSlash(path)

	switch {
	case base == "Makefile" || strings.HasPrefix(base, "Makefile.gen."):
		return true
	case path == ".circleci/config.yml" || strings.HasSuffix(path, "/.circleci/config.yml"):
		return true
	case path == ".circleci/workflows.yml" || strings.HasSuffix(path, "/.circleci/workflows.yml"):
		return true
	case base == ".gitignore":
		return true
	case base == "renovate.json" || base == "renovate.json5":
		return true
	case base == "dependabot.yml":
		return true
	case base == ".pre-commit-config.yaml":
		return true
	case base == ".schema.yaml":
		return true
	case base == "values.schema.json":
		return true
	case base == "aws-ami.yaml.template":
		return true
	case strings.HasPrefix(base, internal.RegenerableFilePrefix):
		return true
	}

	return false
}
