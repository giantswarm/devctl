package followcircleci

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// hooks reads a repository's webhooks on GitHub.
type hooks func(ctx context.Context, owner, repo string) ([]*github.Hook, error)

// circleCI is what the command asks of CircleCI, as the person.
type circleCI interface {
	Me(ctx context.Context) (*circleciclient.User, error)
	Following(ctx context.Context, org, repo string) (bool, error)
	Unfollow(ctx context.Context, org, repo string) error
	Follow(ctx context.Context, org, repo string) error
}

type runner struct {
	stdout io.Writer
	// The seams tests replace: the clients, the clock.
	open     func(ctx context.Context, owner string) (hooks, circleCI, error)
	sleep    func(ctx context.Context, d time.Duration) error
	wait     time.Duration
	interval time.Duration
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return microerror.Mask(r.run(ctx, args[0]))
}

func (r *runner) run(ctx context.Context, arg string) error {
	owner, repo, err := parse(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	slug := owner + "/" + repo
	readHooks, ci, err := r.open(ctx, owner)
	if err != nil {
		return microerror.Mask(err)
	}

	present, err := hasWebhook(ctx, readHooks, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}
	if present {
		fmt.Fprintf(r.stdout, "%s: webhook present, nothing to do\n", slug)
		return nil
	}

	me, err := ci.Me(ctx)
	if err != nil {
		return microerror.Mask(fmt.Errorf("the CircleCI login: %w", err))
	}
	following, err := ci.Following(ctx, owner, repo)
	switch {
	case circleciclient.IsNotFound(err):
		return microerror.Maskf(notFollowedError, "CircleCI knows no project %s: it is set up by the reconciler's circleci step first", slug)
	case err != nil:
		return microerror.Mask(err)
	}
	if following {
		// A follow the login holds already installs nothing: CircleCI
		// installs the hook on a new follow, so the login unfollows first.
		if err := ci.Unfollow(ctx, owner, repo); err != nil {
			return microerror.Mask(err)
		}
		fmt.Fprintf(r.stdout, "%s: unfollowed as %s\n", slug, me.Login)
	}
	if err := ci.Follow(ctx, owner, repo); err != nil {
		return microerror.Mask(err)
	}
	fmt.Fprintf(r.stdout, "%s: followed as %s\n", slug, me.Login)

	for waited := time.Duration(0); ; waited += r.interval {
		present, err := hasWebhook(ctx, readHooks, owner, repo)
		if err != nil {
			return microerror.Mask(err)
		}
		if present {
			fmt.Fprintf(r.stdout, "%s: webhook present\n", slug)
			return nil
		}
		if waited >= r.wait {
			return microerror.Maskf(webhookMissingError,
				"%s: followed as %s, and CircleCI installed no webhook within %s: %s is no GitHub admin of the repository, or the CircleCI grant on GitHub lacks the hook scope (sign out of CircleCI and sign in again through GitHub)",
				slug, me.Login, r.wait, me.Login)
		}
		if err := r.sleep(ctx, r.interval); err != nil {
			return microerror.Mask(err)
		}
	}
}

func hasWebhook(ctx context.Context, read hooks, owner, repo string) (bool, error) {
	hooks, err := read(ctx, owner, repo)
	if err != nil {
		return false, fmt.Errorf("read the webhooks of %s/%s: %w", owner, repo, err)
	}
	return reconcile.HasCircleCIWebhook(hooks), nil
}

// parse splits [OWNER/]REPOSITORY; the owner defaults to giantswarm.
func parse(arg string) (owner, repo string, err error) {
	arg = strings.TrimSpace(arg)
	owner, repo, found := strings.Cut(arg, "/")
	if !found {
		owner, repo = defaultOwner, arg
	}
	if owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", microerror.Maskf(invalidArgError, "expected [OWNER/]REPOSITORY, got %q", arg)
	}
	return owner, repo, nil
}
