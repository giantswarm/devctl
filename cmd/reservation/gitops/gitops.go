// Package gitops gives the reservation commands that write the GitOps repo
// one way to get it: a throwaway clone of --gitops-repo, never a checkout on
// disk that may be behind.
package gitops

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const (
	FlagRepo    = "gitops-repo"
	FlagRepoDir = "repo-dir"

	// DefaultRepo holds the configuration of every Giant Swarm management
	// cluster.
	DefaultRepo = "giantswarm/giantswarm-management-clusters"
)

// Flags select the GitOps repo.
type Flags struct {
	Repo string
	// RepoDir is an existing checkout to work on instead of a clone. It is
	// hidden: the tests use it with a local remote, and nobody should use it
	// to read reservations, as a checkout can be behind.
	RepoDir string
}

func (f *Flags) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Repo, FlagRepo, DefaultRepo, "GitOps repository holding the management cluster, as owner/repo. The command clones it fresh, so no checkout on disk is needed.")
	cmd.Flags().StringVar(&f.RepoDir, FlagRepoDir, "", "Existing checkout of the GitOps repository to use instead of a clone of --"+FlagRepo+".")
	_ = cmd.Flags().MarkHidden(FlagRepoDir)
}

func (f *Flags) Validate() error {
	if f.RepoDir != "" {
		return nil
	}
	_, _, err := SplitRepo(f.Repo)
	return microerror.Mask(err)
}

// SplitRepo splits an owner/repo reference.
func SplitRepo(repo string) (string, string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", microerror.Maskf(invalidFlagError, "--%s must be owner/repo, got %q", FlagRepo, repo)
	}

	return owner, name, nil
}

// Open returns the directory to work on and a cleanup to defer. Without
// --repo-dir it is a throwaway clone of --gitops-repo, so a failed run leaves
// nothing behind and a half-written change can never be pushed. The returned
// context carries the GitHub token for reservation.PushWithRetry.
func (f *Flags) Open(ctx context.Context, stderr io.Writer) (context.Context, string, func(), error) {
	noop := func() {}
	if f.RepoDir != "" {
		return ctx, f.RepoDir, noop, nil
	}

	owner, repo, err := SplitRepo(f.Repo)
	if err != nil {
		return ctx, "", noop, microerror.Mask(err)
	}

	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return ctx, "", noop, err
	}
	token.WarnOnce(stderr)

	client, err := githubclient.New(githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token.Value,
	})
	if err != nil {
		return ctx, "", noop, microerror.Mask(err)
	}

	dir, err := os.MkdirTemp("", "devctl-reservation-*")
	if err != nil {
		return ctx, "", noop, microerror.Mask(err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	err = client.CloneRepository(ctx, owner, repo, dir)
	if err != nil {
		cleanup()
		return ctx, "", noop, githubclient.ExplainNotFound(microerror.Mask(err), authstore.GitHubNotFoundHint(token))
	}

	// The clone keeps the token out of its remote URL, so native git, which
	// pushes and fetches, needs it handed over.
	return reservation.WithGitHubToken(ctx, token.Value), dir, cleanup, nil
}

// Explain turns a clone that is not a GitOps repo into an error that names
// --gitops-repo, the flag to fix. Any other error comes back as it is.
func (f *Flags) Explain(err error) error {
	if f.RepoDir != "" || !reservation.IsNotGitOpsRepo(err) {
		return err
	}

	return microerror.Maskf(invalidFlagError,
		"--%s %s is not the GitOps repo that holds the management cluster configuration: it has no management-clusters directory. Set --%s to that repo, for example %s",
		FlagRepo, f.Repo, FlagRepo, DefaultRepo)
}

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}
