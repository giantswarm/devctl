package apptest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/apptest"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}

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

	var apptestInput *apptest.Apptest
	{
		c := apptest.Config{
			AppName:  r.flag.AppName,
			RepoName: r.flag.RepoName,
			Catalog:  r.flag.Catalog,
		}

		apptestInput, err = apptest.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// Check if the e2e directory already exists
	f, err := filepath.Abs("./tests/e2e")
	if err != nil {
		return microerror.Mask(err)
	}
	if _, err = os.Stat(f); errors.Is(err, os.ErrNotExist) {
		r.logger.Debugf(ctx, "E2E tests don't exist\n")
		var inputs []input.Input
		{
			inputs = append(inputs, apptestInput.CreateApptest()...)
		}

		err = gen.Execute(ctx, inputs...)
		if err != nil {
			return microerror.Mask(err)
		}

		err = updateGoModule(ctx, f)
		if err != nil {
			return microerror.Mask(err)
		}
		err = goModTidy(ctx, f)
		if err != nil {
			return microerror.Mask(err)
		}
	} else {
		r.logger.Log(ctx, "The e2e directory already exists, stopping.\n")
		// We don't want to overwrite existing tests
		return microerror.Mask(fmt.Errorf("the e2e directory already exists"))
	}

	return nil
}

func updateGoModule(ctx context.Context, workingDir string) error {
	return runGo(ctx, workingDir, "get", "-u", "github.com/giantswarm/apptest-framework")
}

func goModTidy(ctx context.Context, workingDir string) error {
	return runGo(ctx, workingDir, "mod", "tidy")
}

func runGo(ctx context.Context, workingDir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = workingDir
	_, err := cmd.Output()
	// A running process killed by CommandContext otherwise reports only a
	// process exit error, hiding the caller's cancellation or deadline.
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
