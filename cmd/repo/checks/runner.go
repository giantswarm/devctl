package checks

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v86/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx, cmd, args))
}

func (r *runner) run(ctx context.Context, _ *cobra.Command, args []string) error {
	parts := strings.SplitN(args[0], "/", 2)
	if len(parts) != 2 {
		return microerror.Maskf(invalidArgError, "expected owner/repo, got %s", args[0])
	}

	owner, repo := parts[0], parts[1]

	if r.flag.Update {
		return microerror.Mask(r.update(ctx, owner, repo))
	}

	return nil
}

func (r *runner) update(ctx context.Context, owner, repo string) error {
	token, found := os.LookupEnv(r.flag.GithubTokenEnvVar)
	if !found {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", r.flag.GithubTokenEnvVar)
	}

	client, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: token,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}

	defaultBranch := repository.GetDefaultBranch()
	underlying := client.GetUnderlyingClient(ctx)

	current, _, err := underlying.Repositories.GetRequiredStatusChecks(ctx, owner, repo, defaultBranch)
	if err != nil {
		if errors.Is(err, github.ErrBranchNotProtected) {
			r.logger.Warnf("%s/%s: branch %q has no protection, skipping", owner, repo, defaultBranch)
			return nil
		}
		return microerror.Mask(err)
	}

	existing := make(map[string]bool)
	var merged []*github.RequiredStatusCheck
	for _, c := range current.GetChecks() {
		merged = append(merged, c)
		existing[c.GetContext()] = true
	}
	for _, name := range r.flag.Checks {
		if !existing[name] {
			merged = append(merged, &github.RequiredStatusCheck{Context: name})
		}
	}

	strict := current.Strict
	_, _, err = underlying.Repositories.UpdateRequiredStatusChecks(ctx, owner, repo, defaultBranch, &github.RequiredStatusChecksRequest{
		Strict: &strict,
		Checks: merged,
	})

	r.logger.Infof("%s/%s: adding %v to required checks on %q", owner, repo, r.flag.Checks, defaultBranch)

	return microerror.Mask(err)
}
