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

	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/apptest"
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

		err = updateGoModule(f)
		if err != nil {
			return microerror.Mask(err)
		}
		err = goModTidy(f)
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

func updateGoModule(workingDir string) error {
	cmd := exec.Command("go", "get", "-u", "github.com/giantswarm/apptest-framework")
	cmd.Dir = workingDir
	_, err := cmd.Output()
	return err
}

func goModTidy(workingDir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = workingDir
	_, err := cmd.Output()
	return err
}
