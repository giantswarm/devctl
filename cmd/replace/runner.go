package replace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
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
		// TODO Consider taking only one last arg which would be a glob.
		globs = args[2:]
	)

	regex, err := regexp.Compile(pattern)
	if err != nil {
		microerror.Mask(err)
	}

	// TODO Files got renamed to globs. This won't compile. We need to take the files using doublestar.Glob.
	for _, file := range files {
		err := filepath.Walk(file, func(file string, info os.FileInfo, err error) error {
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

			// Only include files matching include patterns.
			// In case there are no include patterns, all files are included.
			{
				included, err := globMatchAny(file, r.flag.Include)
				if err != nil {
					return microerror.Mask(err)
				}
				if !included {
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

	content, err := ioutil.ReadAll(f)
	if err != nil {
		return microerror.Mask(err)
	}

	replaced := regex.ReplaceAll(content, []byte(replacement))

	if r.flag.InPlace && bytes.Equal(content, replaced) {
		// Replace entire file content.
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
	} else {
		// Print result to stdout.
		fmt.Fprintf(r.stdout, "%s", replaced)
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
