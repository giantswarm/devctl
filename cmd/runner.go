package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v2/internal/env"
	"github.com/giantswarm/devctl/v2/pkg/project"
	"github.com/giantswarm/devctl/v2/pkg/updater"
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

	var err error

	if project.Version() == env.DevctlUnsafeForceVersion.Val() {
		// User wants to risk his life and use an older version.
		// Not my problem anymore.
		return nil
	}

	var updaterService *updater.Updater
	{
		var cacheDir string
		if !r.flag.NoCache {
			cacheDir = env.ConfigDir.Val()
		}

		config := updater.Config{
			CurrentVersion: project.Version(),
			RepositoryURL:  project.Source(),
			CacheDir:       cacheDir,
		}

		updaterService, err = updater.New(config)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	latestVersion, err := updaterService.GetLatest()
	if updater.IsHasNewVersion(err) {
		fmt.Fprintf(r.stderr, "If you know what you are doing you can disable this check by exporting %s=%s\n", env.DevctlUnsafeForceVersion.Key(), project.Version())
		fmt.Fprintf(r.stderr, "Current version:  %s\n", project.Version())
		fmt.Fprintf(r.stderr, "Latest version:   %s\n", latestVersion)
		fmt.Fprintf(r.stderr, "Please update your %s with \"%s version update\"\n", project.Name(), project.Name())

		return microerror.Mask(err)
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
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
