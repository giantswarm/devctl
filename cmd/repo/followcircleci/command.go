// Package followcircleci is `devctl repo follow-circleci`: the repair of the
// reconciler's finding circleci-webhook-missing, a follow of the CircleCI
// project as the person so CircleCI installs its webhook.
package followcircleci

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/rerun"
)

const (
	name      = "follow-circleci"
	shortDesc = "Follow a repository's CircleCI project as you, so CircleCI installs its missing webhook"
	longDesc  = `Repair the reconciler's finding circleci-webhook-missing: a repository followed
on CircleCI without CircleCI's webhook (https://circleci.com/hooks/github), so
no push and no tag reaches CircleCI.

CircleCI installs that webhook, signed with a secret of its own, only for a
follow by a GitHub admin of the repository whose CircleCI grant carries the
hook scope; the reconciler's follow is not one. This command follows the
project again with your CircleCI login from the keychain (devctl auth login
--circleci-only, Write access): an unfollow first when you follow it already,
then the follow. It then reads the repository's webhooks until CircleCI's is
there, up to a minute.

A repository that carries the webhook is left alone. A webhook still missing
after the follow means your login cannot install it: you are no GitHub admin
of the repository, or your CircleCI grant on GitHub lacks the hook scope
(sign out of CircleCI and sign in again through GitHub to renew it).

GitHub is read as devctl pr wait reads it: the devctl App login for
giantswarm, your own gh login for every other owner.

Examples:
  devctl repo follow-circleci mcp-template
  devctl repo follow-circleci giantswarm/mcp-template`
)

// defaultOwner is the owner of a repository named without one.
const defaultOwner = "giantswarm"

// The webhook check after the follow: CircleCI installs the hook within
// seconds of the follow's answer.
const (
	webhookWait     = time.Minute
	webhookInterval = 5 * time.Second
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	r := &runner{
		stdout:   config.Stdout,
		open:     open,
		sleep:    sleep,
		wait:     webhookWait,
		interval: webhookInterval,
	}

	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [OWNER/]REPOSITORY", name),
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		RunE:  r.Run,
	}
	return c, nil
}

// open builds the two clients from the keychain: GitHub on the identity
// devctl pr wait picks for owner, CircleCI on the person's login.
func open(ctx context.Context, owner string) (hooks, circleCI, error) {
	endpoints := agentcli.EndpointsFromEnv()
	gh, _, err := rerun.NewGitHub(ctx, owner, authstore.RequireGitHub, authexec.PersonGitHub, endpoints, http.DefaultTransport)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}
	ci, err := rerun.NewCircleCI(ctx, authstore.RequireCircleCI, endpoints, http.DefaultTransport, func(string) {})
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}
	client := gh.GetUnderlyingClient(ctx)
	read := func(ctx context.Context, owner, repo string) ([]*github.Hook, error) {
		hooks, _, err := client.Repositories.ListHooks(ctx, owner, repo, &github.ListOptions{PerPage: 100})
		return hooks, err
	}
	return read, ci, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
