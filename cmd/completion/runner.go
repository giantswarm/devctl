package completion

import (
	"context"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

type runner struct {
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var err error
	switch args[0] {
	case "bash":
		err = cmd.Root().GenBashCompletion(os.Stdout)
		if err != nil {
			return microerror.Mask(err)
		}
	case "zsh":
		err = cmd.Root().GenZshCompletion(os.Stdout)
		if err != nil {
			return microerror.Mask(err)
		}
	case "fish":
		err = cmd.Root().GenFishCompletion(os.Stdout, true)
		if err != nil {
			return microerror.Mask(err)
		}
	case "powershell":
		err = cmd.Root().GenPowerShellCompletion(os.Stdout)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}
