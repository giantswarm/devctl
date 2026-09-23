package cmd

import (
	"context"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) PersistentPreRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.persistentPreRun(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.configureLogger(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
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

func (r *runner) persistentPreRun(ctx context.Context, cmd *cobra.Command, args []string) error {
	parentCmd := cmd.Parent()
	if (parentCmd != nil && parentCmd.Name() == "version") || cmd.Name() == "version" {
		return nil
	}
	// An agent-facing command runs the gate itself and reports it in its
	// document.
	if agentcli.IsAgentFacing(cmd.Annotations) {
		return nil
	}

	return microerror.Mask(versiongate.Check(r.flag.NoCache))
}

func (r *runner) configureLogger(ctx context.Context, cmd *cobra.Command, args []string) error {
	level, err := logrus.ParseLevel(r.flag.LogLevel)
	if err != nil {
		return microerror.Mask(err)
	}

	logrus.SetLevel(level)
	logrus.SetOutput(os.Stdout)

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	err := cmd.Help()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
