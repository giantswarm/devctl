package wait

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
requires branches to be up to date. A failure anywhere ends the wait at once, and
so do Actions runs awaiting a member's approval when nothing else is pending.

CircleCI is consulted when the head carries .circleci/config.yml and CircleCI
builds the repository: it has a project there with at least one pipeline. A
repository without either (an upstream fork; a template repository whose
configuration is for the repositories created from it) is judged from GitHub
alone. Tokens come from the keychain (` + "`devctl auth login`" + `); the CircleCI token is
required only when CircleCI is consulted. The GitHub identity follows the owner: the devctl App login for giantswarm,
where the App is installed, your own gh login (gh auth token) for every other
owner; the document's identity says which ("app" or "gh").

Polling is conditional (ETags, a 304 costs no budget) at an interval derived
from the rate-limit headers of the answers, 15 s to 60 s. Without --progress a
heartbeat on stderr says what the wait still waits for, every two minutes and
when it changes.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, number, headSha, baseRef,
checks[{name, source (check_run|status), status, conclusion, url, required}],
circleci{pipelineId, pipelineNumber, workflows[{name, status, url}]} when
consulted, actions[{name, runId, status, conclusion, url}], at a timeout
unfinished[]: what the head was still waiting for, and on red with
--failed-log failedJobs[{name, source (actions|circleci), url, logTail,
logError}]. See docs/pr-wait.md.

--failed-log: on a red verdict, read the log of each failed GitHub Actions
and CircleCI job once and print its last --failed-log-lines lines (default
50) to stderr; a green or pending wait reads no log.

Exit codes:
  0  green
  1  red: a check, status, run or workflow failed; reason names it
  2  timeout before an outcome; unfinished names what was still open
  3  not applicable: draft, closed, merged, conflicting, behind a strict base
  4  a required status context never reported, or Actions runs await a
     member's approval and nothing else is pending
  7  usage or a tooling failure
  8  authentication required; reason names the devctl auth login (or gh auth
     login, for an owner outside giantswarm) to run`
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
		personGitHub:    authexec.PersonGitHub,
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
