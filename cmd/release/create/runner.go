package create

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/release"
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

func (r *runner) run(_ context.Context, cmd *cobra.Command, _ []string) error {
	creationCommand := fmt.Sprintf("%v", strings.Join(os.Args, " "))

	err := release.CreateRelease(r.flag.Name, r.flag.Base, r.flag.Releases, r.flag.Provider, r.flag.Components, r.flag.Apps, r.flag.Overwrite, creationCommand, r.flag.BumpAll, r.flag.Drop, r.flag.Yes, r.flag.Output, r.flag.Verbose, r.flag.ChangesOnly, r.flag.RequestedOnly, r.flag.UpdateExisting, r.flag.PreserveReadme, r.flag.RegenerateReadme, r.flag.ChangelogNoisePatterns)
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}
