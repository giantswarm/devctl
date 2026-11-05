package version

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/project"
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
	_, err := fmt.Fprintf(r.stdout, "Version:        %s\nGit Commit:     %s\nGo Version:     %s\nOS / Arch:      %s / %s\nSource:         %s\n",
		project.Version(), project.GitSHA(), runtime.Version(), runtime.GOOS, runtime.GOARCH, project.Source())
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
