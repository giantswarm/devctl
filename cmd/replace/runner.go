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
		files       = args[2:]
	)

	regex, err := regexp.Compile(pattern)
	if err != nil {
		microerror.Mask(err)
	}

	var ignoredFiles []string
	{
		for _, ignorePattern := range r.flag.Ignore {
			ignored, err := doublestar.Glob(ignorePattern)
			if err != nil {
				return microerror.Mask(err)
			}
			ignoredFiles = append(ignoredFiles, ignored...)
		}
	}

	var includedFiles []string
	{
		for _, includePattern := range r.flag.Include {
			included, err := doublestar.Glob(includePattern)
			if err != nil {
				return microerror.Mask(err)
			}
			includedFiles = append(includedFiles, included...)
		}
	}

	for _, file := range files {
		err := filepath.Walk(file, func(file string, info os.FileInfo, err error) error {
			// Ignore file if present in ignored files list.
			if contains(file, ignoredFiles) {
				return nil
			}
			// Ignore files which are not in include files list, when the list is not empty.
			if len(includedFiles) > 0 && !contains(file, includedFiles) {
				return nil
			}
			if err != nil {
				return microerror.Mask(err)
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

func contains(file string, files []string) bool {
	for _, f := range files {
		if file == f {
			return true
		}
	}
	return false
}
