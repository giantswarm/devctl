package replace

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
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
	regex, err := regexp.Compile(args[0])
	if err != nil {
		microerror.Mask(err)
	}

	for _, file := range args[2:] {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			microerror.Mask(err)
		}
		fmt.Fprintf(r.stderr, "> file %s\n", file)
		replaced := regex.ReplaceAll(content, []byte(args[1]))
		if r.flag.inPlace {
			err := ioutil.WriteFile(file, replaced, 0644)
			if err != nil {
				microerror.Mask(err)
			}
		} else {
			fmt.Fprintf(r.stdout, "%s", replaced)
		}
	}
	return nil
}
