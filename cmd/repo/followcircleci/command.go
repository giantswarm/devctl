// Package followcircleci is `devctl repo follow-circleci`: a repository set
// up on a CircleCI GitHub App pipeline by the person who runs it, the fix of
// the reconciler's finding circleci-pipeline-missing.
package followcircleci

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
)

const (
	name      = "follow-circleci"
	shortDesc = "Set a repository up on a CircleCI GitHub App pipeline, as you"
	longDesc  = `Set a repository up on a CircleCI GitHub App pipeline with your CircleCI
login: the fix of the reconciler's finding circleci-pipeline-missing, and the
step ` + "`devctl repo watch`" + ` takes for the creator of a new repository.

CircleCI has no service accounts, and the reconciler holds no CircleCI
identity: the pipeline is created by a person. Afterwards nobody is
load-bearing. The GitHub App checks out, receives the pushes and posts the
statuses (as circleci-app[bot], under the same ci/circleci: contexts), so the
pipeline keeps building when whoever created it leaves -- unlike an OAuth
follow, which builds through the follower's GitHub grant.

The command, idempotent:
  1. finds the project's GitHub App pipeline, or creates <repository>-app
     (config and checkout through the GitHub App, .circleci/config.yml);
  2. gives it an all-pushes trigger (branches and tags) unless it has one,
     and switches the project's setup workflows on when they are off (the
     generated dynamic configuration needs them; a refused read is a
     warning);
  3. with --migrate, deletes the OAuth pipeline's triggers, so a push builds
     once, on the App; without it a remaining OAuth trigger is reported;
  4. runs every release whose tag carries no CircleCI status of its own
     build -- the latest vX.Y.Z release, or the newest of every line on a
     multi-image repository; a tag cut before the trigger existed reached no
     pipeline, and one an OAuth follow built is never run again -- then the
     default branch, when anything changed or it carries no status of the
     GitHub App pipeline;
  5. waits for those runs to settle (--wait=false returns at once).
The exit is 0 when every run is green.

The CircleCI login is the keychain's (devctl auth login --circleci-only),
granted Write on the consent page: a Read-only login is refused on the first
write. GitHub is read as devctl pr wait reads it: the devctl App login for
giantswarm, your own gh login for every other owner.

With --pending, instead of one repository: every repository of your teams
the inventory reports circleci-pipeline-missing for (giantswarm-repo-manager's
list, scope mine; declared, in production, unarchived), set up without --migrate and
without waiting; --output json prints its document. After any devctl command,
at most once every six hours, devctl runs that pass by itself, quietly, with
the logins the keychain holds; any non-empty value of DEVCTL_NO_CIRCLECI_PENDING turns it off.

Examples:
  devctl repo follow-circleci my-service
  devctl repo follow-circleci --pending
  devctl repo follow-circleci giantswarm/my-service --migrate
  devctl repo follow-circleci my-service --wait=false`
)

// defaultOwner is the owner of a repository named without one.
const defaultOwner = "giantswarm"

// The wait for the runs: a pipeline reads green in two to ten minutes.
const (
	defaultTimeout = 30 * time.Minute
	pollInterval   = 10 * time.Second
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

type flag struct {
	client.Flags
	Migrate bool
	Pending bool
	Wait    bool
	Timeout time.Duration
	// WaitSet says --wait or --timeout was given, which --pending refuses.
	WaitSet bool
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	r := &runner{
		flag:        &flag{},
		stdout:      config.Stdout,
		stderr:      config.Stderr,
		open:        circleciapp.OpenAsPerson,
		openSession: client.Open,
		sleep:       sleep,
		interval:    pollInterval,
	}
	return newCommand(r), nil
}

// newCommand is the command running r; tests give r fake clients.
func newCommand(r *runner) *cobra.Command {
	f := r.flag
	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [OWNER/]REPOSITORY | --pending", name),
		Short: shortDesc,
		Long:  longDesc,
		// validate counts the arguments, so a wrong call under -o json is
		// the document.
		Args: cobra.ArbitraryArgs,
		RunE: r.Run,
	}
	c.Flags().BoolVar(&f.Migrate, "migrate", false, "Also delete the OAuth pipeline's triggers, so a push builds once, on the GitHub App pipeline.")
	c.Flags().BoolVar(&f.Wait, "wait", true, "Wait for the runs the command starts to settle; one repository only, --pending never waits.")
	c.Flags().DurationVar(&f.Timeout, "timeout", defaultTimeout, "How long to wait for the runs; one repository only.")
	c.Flags().BoolVar(&f.Pending, "pending", false, "Instead of one repository, set up every repository of your teams the inventory reports circleci-pipeline-missing for, without waiting for the runs.")
	f.Init(c)
	c.Flags().Lookup("output").Usage = "Output format with --pending: text, or json for the command's document (envelope, pending, repositories)."
	c.SetFlagErrorFunc(r.FlagError)
	return c
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
