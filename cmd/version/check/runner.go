package check

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/internal/env"
	"github.com/giantswarm/devctl/pkg/project"
	"github.com/giantswarm/devctl/pkg/updater"
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
	var err error

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

	_, err = updaterService.GetLatest()
	if updater.IsHasNewVersion(err) {
		color.New(color.Bold, color.FgYellow).Fprintf(r.stderr, "There's a new version available!\n")
		fmt.Fprintf(r.stderr, "Run \"%s version update\" to update to the latest version.\n", project.Name())

		os.Exit(125)
	} else if updater.IsVersionNotFound(err) {
		color.New(color.Bold, color.FgRed).Fprintf(r.stderr, "Checking for the latest version failed or your platform is unsupported.\n")
		fmt.Fprintf(r.stderr, "Make sure your GitHub token has access to the %s repository.\n", project.Name())

		return microerror.Mask(err)
	} else if err != nil {
		return microerror.Mask(err)
	}

	color.New(color.Bold, color.FgGreen).Fprintf(r.stdout, "You are already using the latest version.\n")
	fmt.Fprintf(r.stdout, "There are no newer versions available.\n")

	return nil
}
