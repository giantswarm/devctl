package pr

import (
	"context"
	"io"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stderr io.Writer
	stdout io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	return r.run(ctx, cmd, args)
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	// Parent command, prints help by default if no subcommand is given.
	cmd.Help()
	return nil
} 