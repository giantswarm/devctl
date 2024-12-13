package renovate

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/renovate"
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

	var renovateInput *renovate.Renovate
	{
		c := renovate.Config{
			Interval: r.flag.Interval,
			Language: r.flag.Language,
		}

		renovateInput, err = renovate.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var inputs []input.Input
	{
		inputs = append(inputs, renovateInput.CreateRenovate())
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	f, err := filepath.Abs("./.github/dependabot.yml")
	if err != nil {
		return microerror.Mask(err)
	}

	err = os.Remove(f)
	if os.IsNotExist(err) {
		// no-op
	} else if err != nil {
		return microerror.Mask(err)
	}

	// Clean up old `renovate.json` in favour of new `renovate.json5`
	f, err = filepath.Abs("./renovate.json")
	if err != nil {
		return microerror.Mask(err)
	}

	err = os.Remove(f)
	if os.IsNotExist(err) {
		// no-op
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
