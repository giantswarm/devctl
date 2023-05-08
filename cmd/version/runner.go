package version

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v2/pkg/project"
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
	fmt.Fprintf(r.stdout, "Version:        %s\n", project.Version())
	fmt.Fprintf(r.stdout, "Git Commit:     %s\n", project.GitSHA())
	fmt.Fprintf(r.stdout, "Go Version:     %s\n", runtime.Version())
	fmt.Fprintf(r.stdout, "OS / Arch:      %s / %s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(r.stdout, "Source:         %s\n", project.Source())

	return nil
}
