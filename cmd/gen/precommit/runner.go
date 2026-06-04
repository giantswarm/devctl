package precommit

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit"
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

	// When --repo-name is not provided, auto-detect it from the local go.mod
	// module path (e.g. github.com/giantswarm/devctl/v8). This value is used as
	// the goimports -local prefix in the generated pre-commit config.
	if r.flag.RepoName == "" {
		content, err := os.ReadFile("go.mod")
		if err != nil {
			return fmt.Errorf("failed to read go.mod: %w", err)
		}
		modulePath := modfile.ModulePath(content)
		if modulePath == "" {
			return fmt.Errorf("failed to parse module path from go.mod: %w", err)
		}
		r.flag.RepoName = modulePath
	}

	var precommitInput *precommit.PreCommit
	{
		c := precommit.Config{
			Language:         r.flag.Language,
			Flavors:          r.flag.Flavors,
			RepoName:         r.flag.RepoName,
			K8sSchemaVersion: r.flag.K8sSchemaVersion,
		}

		precommitInput, err = precommit.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var inputs []input.Input
	{
		inputs = append(inputs, precommitInput.CreatePreCommitConfig())
		inputs = append(inputs, precommitInput.CreatePreCommitAction())
		inputs = append(inputs, precommitInput.CreateSchemaYamlInputs()...)
		inputs = append(inputs, precommitInput.CreateHelmReadmeInputs()...)
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
