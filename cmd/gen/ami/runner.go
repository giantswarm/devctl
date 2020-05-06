package ami

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/ami"
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
	c := ami.Config{
		Arch:           r.flag.Arch,
		Channel:        r.flag.Channel,
		ChinaDomain:    r.flag.ChinaDomain,
		Dir:            r.flag.Dir,
		MinimumVersion: r.flag.MinimumVersion,
		PrimaryDomain:  r.flag.PrimaryDomain,
	}

	amiFile, err := ami.NewAMI(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = gen.Execute(
		ctx,
		amiFile,
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
