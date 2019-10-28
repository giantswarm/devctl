package replace

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"

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

	replacer := func(src []byte) []byte {
		return regex.ReplaceAll(src, []byte(replacement))
	}

	for _, file := range files {
		fmt.Fprintf(r.stderr, "> file %s\n", file)
		err := r.processFile(file, replacer)
		if err != nil {
			return microerror.Mask(err)
		}
	}
	return nil
}

func (r *runner) processFile(fileName string, replacer func(src []byte) []byte) error {
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

	replaced := replacer(content)

	if r.flag.InPlace {
		// Replace entire file content.
		err := f.Truncate(0)
		if err != nil {
			return microerror.Mask(err)
		}
		_, err = f.Write(replaced)
		if err != nil {
			return microerror.Mask(err)
		}
	} else {
		// Print result to stdout.
		fmt.Fprintf(r.stdout, "%s", replaced)
	}

	return nil
}
