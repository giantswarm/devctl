package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

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

func (r *runner) Run(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		fmt.Fprintf(r.stderr, "%s\n", err.Error())
		os.Exit(2)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		fmt.Fprintf(r.stderr, "%#v\n", err)
		os.Exit(1)
	}
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	err := cmd.Help()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
