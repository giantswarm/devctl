package replace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/bmatcuk/doublestar"
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
	var (
		pattern     = args[0]
		replacement = args[1]
		globs       = args[2:]
	)

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return microerror.Mask(err)
	}

	matches := make(map[string]struct{})
	{
		for _, g := range globs {
			ms, err := doublestar.Glob(g)
			if err != nil {
				return microerror.Mask(err)
			}

			for _, m := range ms {
				matches[m] = struct{}{}
			}
		}
	}

	for file := range matches {
		err := filepath.Walk(file, func(file string, info os.FileInfo, err error) error {
			if err != nil {
				return microerror.Mask(err)
			}

			// Skip files matching any ignore pattern.
			{
				ignored, err := globMatchAny(file, r.flag.Ignore)
				if err != nil {
					return microerror.Mask(err)
				}
				if ignored {
					if info.IsDir() {
						return filepath.SkipDir
					}

					return nil
				}
			}

			if info.IsDir() {
				return nil
			}

			fmt.Fprintf(r.stderr, "Processing file %q.\n", file)
			err = r.processFile(file, regex, replacement)
			if err != nil {
				return microerror.Mask(err)
			}

			return nil
		})
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func (r *runner) processFile(fileName string, regex *regexp.Regexp, replacement string) error {
	// Write permission is only needed in case the file needs to be changed.
	flag := os.O_RDONLY
	if r.flag.InPlace {
		flag = os.O_RDWR
	}

	// Open file, do not attempt to create it (last argument for file permission is ignored in this case).
	f, err := os.OpenFile(fileName, flag, 0)
	if err != nil {
		return microerror.Mask(err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return microerror.Mask(err)
	}

	replaced := regex.ReplaceAll(content, []byte(replacement))

	// When --inplace flag isn't specified print replacement to stdout and
	// return.
	if !r.flag.InPlace {
		fmt.Fprintf(r.stdout, "%s", replaced)

		return nil
	}

	// If replaced content is the same there is no reason to override the
	// file so return early.
	if bytes.Equal(content, replaced) {
		return nil
	}

	// Replace entire file content.
	{
		err := f.Truncate(0)
		if err != nil {
			return microerror.Mask(err)
		}

		n, err := f.WriteAt(replaced, 0)
		if err != nil {
			return microerror.Mask(err)
		}
		if n < len(replaced) {
			return microerror.Maskf(executionFailedError, "short write to %#q only %d of %d bytes written", f.Name(), n, len(replaced))
		}
	}

	return nil
}

func globMatchAny(file string, patterns []string) (bool, error) {
	for _, pattern := range patterns {
		ok, err := doublestar.PathMatch(pattern, file)
		if err != nil {
			return false, microerror.Mask(err)
		}
		if ok {
			return true, nil
		}
	}

	return false, nil
}
