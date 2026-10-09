package rerun

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "rerun <owner/repo> <tag>"
	description = "Rerun the failed CircleCI workflows of a tag's pipeline from failed; one JSON document, an exit code."
	long        = `Rerun every failed CircleCI workflow of a tag's pipeline from failed: its
failed jobs and the jobs that depend on them run again, the passed ones are
kept. A release whose tag pipeline failed on a transient cause (a push race, a
flaky download) is completed this way, without a new tag.

The pipeline is the newest one CircleCI built for the tag; a version given as
X.Y.Z or vX.Y.Z finds the tag in either spelling. Of each workflow name the
newest run counts. A finished workflow that failed is rerun, a canceled one
when a job of it failed; one that is still running is not, CircleCI reruns a
finished workflow only. The rerun is started, not waited for: devctl release wait
waits for it.

A rerun is a write: it takes a CircleCI login that granted Write access
(devctl auth login --circleci-only, Write on the consent page); a login that
granted Read access only is exit 8 naming that login. GitHub is not read.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, tag, headSha, pipeline{id, number, url},
workflows[{name, id, status, url, failedJobs[], outcome
(rerun|running|no_failed_job|nothing_to_rerun|refused), rerunId, rerunUrl}]. See
docs/pr-rerun.md.

Exit codes:
  0  at least one workflow is rerun from failed
  3  not applicable: no pipeline for the tag, or no failed workflow
  5  refused: nothing finished failed and a workflow is still running
  7  usage or a tooling failure
  8  authentication required: no CircleCI login, or one without Write access`
	example = `  devctl release rerun giantswarm/devctl v8.123.0
  devctl release rerun giantswarm/devctl v8.123.0 && /home/teemow/.go/bin/beekeeper gate -- devctl release wait giantswarm/devctl v8.123.0`
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
