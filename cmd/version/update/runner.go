package update

import (
	"context"
	"fmt"
	"io"

	"github.com/fatih/color"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/updater"
)

type runner struct {
	flag       *flag
	logger     micrologger.Logger
	stdout     io.Writer
	stderr     io.Writer
	newUpdater func(bool, io.Writer) (updateService, error)
}

type updateService interface {
	GetLatestFromSource() (string, error)
	InstallLatestWithResult() (updater.InstallResult, error)
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
	newUpdater := r.newUpdater
	if newUpdater == nil {
		newUpdater = func(noCache bool, warnings io.Writer) (updateService, error) {
			return versiongate.NewUpdater(noCache, warnings)
		}
	}
	updaterService, err := newUpdater(r.flag.NoCache, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}

	// Get the latest version number.
	latestVersion, err := updaterService.GetLatestFromSource()
	if updater.IsHasNewVersion(err) {
		fmt.Fprintf(r.stdout, "Update to %s has been started.\n", latestVersion)
		fmt.Fprintf(r.stdout, "Fetching latest built binary...\n")

		// Install the latest available version.
		result, err := updaterService.InstallLatestWithResult()
		if err != nil {
			return microerror.Mask(err)
		}

		if !result.Installed {
			fmt.Fprintf(r.stdout, "No newer version remains available; the installed binary is unchanged.\n")
			return nil
		}
		_, _ = color.New(color.FgGreen).Fprintf(r.stdout, "Updated successfully to %s.\n", result.Version)

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
