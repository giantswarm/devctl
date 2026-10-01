package setlifecycle

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	open   client.Opener
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(r.run(context.Background(), args[0], args[1]))
}

func (r *runner) run(ctx context.Context, arg, lifecycle string) error {
	repository, err := client.Repository(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	if err := r.flag.validateLifecycle(repository, lifecycle); err != nil {
		return microerror.Mask(err)
	}
	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	args := map[string]any{"repository": repository, "lifecycle": lifecycle}
	if r.flag.Confirm != "" {
		args["confirm"] = r.flag.Confirm
	}
	return microerror.Mask(write.Run(ctx, session, r.stdout, &r.flag.Flags, manager.ToolSetLifecycle, args))
}
