package renovate

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/gitremote"
	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/renovate"
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

	// The optional repo-owned renovate-custom.json5 is referenced from the
	// generated config's `extends` when present. Its presence is the signal;
	// devctl never generates or touches the file itself.
	_, statErr := os.Stat("renovate-custom.json5")
	hasCustomConfig := statErr == nil

	// The repository name only goes into the renovate-custom.json5 extends
	// entry. Without --repo-name it is read from the origin remote, never
	// from the directory name: a worktree or a second clone named anything
	// else would extend a preset Renovate cannot resolve.
	repoName := r.flag.RepoName
	if repoName == "" && hasCustomConfig {
		repoName, err = gitremote.RepoName(ctx, ".")
		if err != nil {
			return microerror.Maskf(invalidFlagError, "renovate-custom.json5 is extended as github>%s/<name>:renovate-custom.json5, and the repository name cannot be read from the origin remote: %s; pass --%s <name>", gitremote.Owner, err, flagRepoName)
		}
	}

	var renovateInput *renovate.Renovate
	{
		c := renovate.Config{
			Interval:          r.flag.Interval,
			Language:          r.flag.Language,
			Reviewers:         r.flag.Reviewers,
			CircleCIGenerated: r.flag.CircleCIGenerated,
			RepoName:          repoName,
			HasCustomConfig:   hasCustomConfig,
			Deprecated:        r.flag.Deprecated,
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
