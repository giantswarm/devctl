package renovate

import (
	"context"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/gitremote"
	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/renovate"
	pkgrenovate "github.com/giantswarm/devctl/v8/pkg/renovate"
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

	// A GitHub fork is scanned only when renovate.json admits it, so the fork
	// flavour writes that file and the other flavours renovate.json5; the
	// one not written is removed.
	fork := r.flag.Flavours.Contains(gen.FlavourFork)
	configFile, staleFile := "renovate.json5", "renovate.json"
	if fork {
		configFile, staleFile = staleFile, configFile

		// A fork line that keeps Renovate off on purpose keeps its opt-out:
		// the generated config would admit the fork and turn Renovate on.
		for _, f := range []string{configFile, staleFile} {
			content, err := os.ReadFile(f) // #nosec G304 -- one of the two fixed Renovate config names
			if err != nil && !os.IsNotExist(err) {
				return microerror.Mask(err)
			}
			if pkgrenovate.Disables(content) {
				return microerror.Maskf(invalidFlagError, "%s turns Renovate off for this fork line (`enabled: false`) and stays as it is: Renovate skips a fork without a renovate.json that sets forkProcessing to \"enabled\" anyway; remove the opt-out first to have the fork line scanned", f)
			}
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
			Extends:           r.flag.Extends,
			Fork:              fork,
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

	// Renovate replaces Dependabot's version updates, except on a fork line:
	// its .github/dependabot.yml is upstream's, part of the tree it carries.
	stale := []string{staleFile}
	if !fork {
		stale = append(stale, ".github/dependabot.yml")
	}
	for _, f := range stale {
		err = os.Remove(f)
		if err != nil && !os.IsNotExist(err) {
			return microerror.Mask(err)
		}
	}

	return nil
}
