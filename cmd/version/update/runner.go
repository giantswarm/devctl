package update

import (
	"context"
	"fmt"
	"io"

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

	// Get the latest version number.
	latestVersion, err := updaterService.GetLatest()
	if updater.IsHasNewVersion(err) {
		fmt.Fprintf(r.stdout, "Update to %s has been started.\n", latestVersion)
		fmt.Fprintf(r.stdout, "Fetching latest built binary...\n")

		// Install the latest available version.
		err = updaterService.InstallLatest()
		if err != nil {
			return microerror.Mask(err)
		}

		color.New(color.FgGreen).Fprintf(r.stdout, "Updated successfully.\n")

		return nil
	} else if updater.IsVersionNotFound(err) {
		fmt.Fprintf(r.stderr, "Checking for the latest version failed or your platform is unsupported.\n")
		fmt.Fprintf(r.stderr, "Make sure your GitHub token has access to the %s repository.\n", project.Name())

		return microerror.Mask(err)
	} else if err != nil {
		return microerror.Mask(err)
	} else {
		fmt.Fprintf(r.stdout, "You are already using the latest version.\n")

		return nil
	}

}
