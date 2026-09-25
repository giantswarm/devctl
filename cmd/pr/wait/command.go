package wait

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "wait <owner/repo> <number>"
	description = "Block until the pull request's head is green, red or cannot become green; one JSON document, an exit code."
	long        = `Block until the outcome of a pull request's CI is known, then print one JSON
document on stdout and exit with a code that says what it is.

Green is what the merge box sees, not what a check list shows early: every check
run and commit status of the head (the latest run per name, so a rerun replaces a
stale failed run), every CircleCI workflow of the head revision (read from
CircleCI: a job behind requires: has posted nothing to GitHub until it starts),
no GitHub Actions run of the head queued, in progress, waiting or awaiting a
maintainer's approval, and every status context the base requires reported.

The wait ends before it starts for a pull request no CI can turn green: a draft,
a closed or merged one, one conflicting with its base, one behind a base that
requires branches to be up to date. A failure anywhere ends the wait at once.

CircleCI is consulted when the head carries .circleci/config.yml and CircleCI
builds the repository: it has a project there with at least one pipeline. A
repository without either (an upstream fork; a template repository whose
configuration is for the repositories created from it) is judged from GitHub
alone. Tokens come from the keychain (` + "`devctl auth login`" + `); the CircleCI token is
required only when CircleCI is consulted.

Polling is conditional (ETags, a 304 costs no budget) at an interval derived
from the rate-limit headers of the answers, 15 s to 60 s.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, number, headSha, baseRef,
checks[{name, source (check_run|status), status, conclusion, url, required}],
circleci{pipelineId, pipelineNumber, workflows[{name, status, url}]} when
consulted, actions[{name, runId, status, conclusion, url}], and at a timeout
unfinished[]: what the head was still waiting for. See docs/pr-wait.md.

Exit codes:
  0  green
  1  red: a check, status, run or workflow failed; reason names it
  2  timeout before an outcome; unfinished names what was still open
  3  not applicable: draft, closed, merged, conflicting, behind a strict base
  4  a required status context never reported within the timeout
  7  usage or a tooling failure
  8  authentication required; reason names the devctl auth login to run`
	example = `  devctl pr wait giantswarm/devctl 2277
  devctl pr wait giantswarm/devctl 2277 --timeout 45m --progress`
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

	f := &flag{}

	r := &runner{
		gate:            versiongate.Check,
		flag:            f,
		stderr:          config.Stderr,
		stdout:          config.Stdout,
		requireGitHub:   authstore.RequireGitHub,
		requireCircleCI: authstore.RequireCircleCI,
		renewGitHub:     authstore.RenewGitHubToken,
		endpoints:       agentcli.EndpointsFromEnv,
		clock:           agentcli.SystemClock,
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

	f.Init(c)

	return c, nil
}
