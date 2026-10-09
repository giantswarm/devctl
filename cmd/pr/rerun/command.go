package rerun

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "rerun <owner/repo> <number>"
	description = "Rerun the failed CircleCI workflows of the pull request's head from failed; one JSON document, an exit code."
	long        = `Rerun every failed CircleCI workflow of a pull request's head from failed:
its failed jobs and the jobs that depend on them run again, the passed ones
are kept. The fix for a job that failed on a transient cause (a push race, a
flaky download) without an empty commit that rebuilds everything.

The pipeline is the newest one CircleCI built for the head revision, on the
head's branch (pull/<number> for a head in a fork), the one devctl pr wait
reads. Of each workflow name the newest run counts. A finished workflow that
failed is rerun, a canceled one when a job of it failed; one that is still
running is not, CircleCI reruns a finished workflow only. The rerun is started, not
waited for: devctl pr wait waits for it.

A rerun is a write: it takes a CircleCI login that granted Write access
(devctl auth login --circleci-only, Write on the consent page); a login that
granted Read access only is exit 8 naming that login. The GitHub identity
follows the owner like devctl pr wait: the devctl App login for giantswarm,
your own gh login for every other owner.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, number, headSha,
pipeline{id, number, url}, workflows[{name, id, status, url, failedJobs[],
outcome (rerun|running|no_failed_job|nothing_to_rerun|refused), rerunId, rerunUrl}],
identity. See docs/pr-rerun.md.

Exit codes:
  0  at least one workflow is rerun from failed
  3  not applicable: no pipeline for the head, or no failed workflow
  5  refused: nothing finished failed and a workflow is still running
  7  usage or a tooling failure
  8  authentication required: no CircleCI login, or one without Write access`
	example = `  devctl pr rerun giantswarm/devctl 2277
  devctl pr rerun giantswarm/devctl 2277 && /home/teemow/.go/bin/beekeeper gate -- devctl pr wait giantswarm/devctl 2277`
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
		gate:            versiongate.Check,
		stdout:          config.Stdout,
		requireGitHub:   authstore.RequireGitHub,
		personGitHub:    authexec.PersonGitHub,
		requireCircleCI: authstore.RequireCircleCI,
		endpoints:       agentcli.EndpointsFromEnv,
	}

	c := &cobra.Command{
		Use:     name,
		Short:   description,
		Long:    long,
		Example: example,
		// The arguments are checked by the runner: a usage error is exit 7
		// with a document, like every other outcome.
		Args:        cobra.ArbitraryArgs,
		RunE:        r.Run,
		Annotations: agentcli.AgentFacing(),
	}
	c.SetFlagErrorFunc(r.FlagError)

	return c, nil
}
