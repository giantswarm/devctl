package precommit

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/golangci"
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
	repoName := r.flag.RepoName
	if repoName == "" && r.flag.Language == "go" {
		content, err := os.ReadFile("go.mod")
		if err != nil {
			return fmt.Errorf("failed to read go.mod (run from repo root or pass --%s): %w", flagRepoName, err)
		}

		mf, err := modfile.Parse("go.mod", content, nil)
		if err != nil {
			return fmt.Errorf("failed to parse go.mod (fix go.mod or pass --%s): %w", flagRepoName, err)
		}
		if mf.Module == nil || mf.Module.Mod.Path == "" {
			return fmt.Errorf("go.mod does not declare a module path (missing 'module' directive); alternatively pass --%s", flagRepoName)
		}

		repoName = mf.Module.Mod.Path
	} else if r.flag.Language == "go" && !strings.Contains(repoName, "/") {
		// The flag still accepts a bare repository name under giantswarm.
		// The input template consumes the full goimports import prefix.
		repoName = "github.com/giantswarm/" + repoName
	}

	var precommitInput *precommit.PreCommit
	{
		c := precommit.Config{
			Language:         r.flag.Language,
			Flavors:          r.flag.Flavors,
			RepoName:         repoName,
			K8sSchemaVersion: r.flag.K8sSchemaVersion,
			GoGenerate:       r.flag.GoGenerate,
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
		// Must run after CreateSchemaYamlInputs: it reads helm/<chart>/values.yaml and
		// the just-generated zz_generated.app-platform.values.yaml back off disk.
		inputs = append(inputs, precommitInput.CreateValuesSchemaInputs()...)
		inputs = append(inputs, precommitInput.CreateHelmReadmeInputs()...)
		if r.flag.Language == "go" {
			inputs = append(inputs, golangci.NewInput("."))
		}
	}

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
