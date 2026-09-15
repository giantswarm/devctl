package reserve

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/go-git/go-git/v5"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
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
	token := env.GitHubToken.Val()
	if token == "" {
		return microerror.Maskf(envVarNotFoundError,
			"no GitHub token found. Set DEVCTL_GITHUB_TOKEN, GITHUB_TOKEN or OPSCTL_GITHUB_TOKEN")
	}

	owner, repo, err := splitRepo(r.flag.GitOpsRepo)
	if err != nil {
		return microerror.Mask(err)
	}

	client, err := githubclient.New(githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	// A throwaway clone, so a failed run leaves nothing behind to clean up and a
	// half-written reservation can never be pushed.
	dir, err := os.MkdirTemp("", "devctl-reservation-*")
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	err = client.CloneRepository(ctx, owner, repo, dir)
	if err != nil {
		return microerror.Mask(err)
	}

	result, err := reservation.Reserve(reservation.Request{
		RepoDir:     dir,
		Cluster:     r.flag.Cluster,
		App:         r.flag.App,
		Branch:      r.flag.Branch,
		User:        r.flag.User,
		PullRequest: r.flag.PullRequest,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	branch, err := defaultBranch(dir)
	if err != nil {
		return microerror.Mask(err)
	}
	err = client.Push(ctx, branch)
	if err != nil {
		return microerror.Mask(err)
	}

	_, _ = fmt.Fprintf(r.stdout, "Reserved %s on %s for %s until %s.\n",
		r.flag.App, r.flag.Cluster, r.flag.User, result.Until.Format("2006-01-02 15:04 MST"))
	_, _ = fmt.Fprintf(r.stdout, "Source %s follows %s\n", result.SourceName, result.SemverFilter)
	_, _ = fmt.Fprintf(r.stdout, "Commit %s on %s/%s@%s\n", result.Commit, owner, repo, branch)

	return nil
}

// defaultBranch returns the branch the clone checked out, so the push does not
// depend on a guess about main or master.
func defaultBranch(dir string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", microerror.Mask(err)
	}
	head, err := repo.Head()
	if err != nil {
		return "", microerror.Mask(err)
	}
	if !head.Name().IsBranch() {
		return "", microerror.Maskf(invalidConfigError, "the clone is not on a branch (%s)", head.Name())
	}

	return head.Name().Short(), nil
}
