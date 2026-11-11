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

A workflow running (or failing) longer than --wedged-after (2h), far beyond any
tag build, has jobs hung on CircleCI's side: it is canceled, read until the
cancel lands (at most two minutes), and rerun from failed, from start where
CircleCI offers no rerun from failed. Where CircleCI does not honour the cancel,
a fresh pipeline for the tag is triggered through the CircleCI API instead, once
per tag. --wedged-after 0 leaves running workflows alone.

A pipeline that never got a workflow (its setup workflow done and the
continuation never created, or pending without a workflow) has nothing to
rerun: once it is five minutes old, the tag's push webhook delivery is sent
again through GitHub, so CircleCI creates a new pipeline for the tag, which
devctl release wait reads. Once per tag: a delivery sent again before is not
sent a second time. GitHub is read only then, with the identity devctl pr
wait uses (the devctl App login for giantswarm, your own gh login elsewhere);
a 403 names the permission that identity lacks (the App's repository
permission Webhooks: read and write). Where the push cannot be sent again (it
was sent again before, or the webhook or its delivery is gone, as for a
pipeline that errored before any workflow), a fresh pipeline for the tag is
triggered through the CircleCI API instead. Once per tag: a pipeline the API
triggered gets no second one.

A rerun is a write: it takes a CircleCI login that granted Write access
(devctl auth login --circleci-only, Write on the consent page); a login that
granted Read access only is exit 8 naming that login.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, tag, headSha, pipeline{id, number, url},
workflows[{name, id, status, url, failedJobs[], outcome
(rerun|running|no_failed_job|nothing_to_rerun|refused), rerunId, rerunUrl,
canceled, rerunFrom (failed|start)}],
redelivery{hookId, hookUrl, deliveryId, guid, deliveredAt, ref, after, outcome
(redelivered|already_redelivered|refused), redeliveredAt}, trigger{outcome
(triggered|already_triggered|refused), pipeline{id, number, url}}, identity. See
docs/pr-rerun.md.

Exit codes:
  0  at least one workflow is rerun from failed, a wedged one canceled and
     rerun, the push webhook redelivered, or a fresh pipeline triggered
  3  not applicable: no pipeline for the tag, or no failed workflow
  5  refused: nothing finished failed and a workflow is still running within
     --wedged-after, a
     pipeline without a workflow younger than five minutes, or one the API
     triggered itself
  7  usage or a tooling failure
  8  authentication required: no CircleCI login, or one without Write access;
     a GitHub identity without webhook access`
	example = `  devctl release rerun giantswarm/devctl v8.123.0
  devctl release rerun giantswarm/devctl v8.123.0 --wedged-after 1h
  devctl release rerun giantswarm/devctl v8.123.0 && devctl release wait giantswarm/devctl v8.123.0`
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
		flag:            f,
		gate:            versiongate.Check,
		stdout:          config.Stdout,
		requireGitHub:   authstore.RequireGitHub,
		personGitHub:    authexec.PersonGitHub,
		requireCircleCI: authstore.RequireCircleCI,
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
	f.Init(c)
	c.SetFlagErrorFunc(r.FlagError)

	return c, nil
}
