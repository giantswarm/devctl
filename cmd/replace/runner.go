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

	content, err := ioutil.ReadFile(args[2])
	if err != nil {
		microerror.Mask(err)
	}

	replaced := regex.ReplaceAll(content, []byte(args[1]))
	fmt.Fprintf(r.stdout, "%s", replaced)
	return nil
}
