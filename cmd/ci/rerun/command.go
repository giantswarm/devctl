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
	name        = "rerun <owner/repo> <workflow id>"
	description = "Rerun one CircleCI workflow, once an hour; one JSON document, an exit code."
	long        = `Rerun one CircleCI workflow: every job of it, or with --from-failed its
failed jobs and the jobs that depend on them, the passed ones kept. The
workflow is named by its id, the one devctl ci jobs, devctl pr wait and the
CircleCI UI show, and has to belong to the repository.

One rerun per hour: a rerun is a second run of the workflow's name in the
same pipeline, and when the newest run of the name is such a rerun made less
than an hour ago, the call is refused (exit 5) naming it and when the hour
ends, whichever run of the name was given, whoever started the rerun. An
agent that reruns on every poll therefore cannot loop; devctl ci jobs shows
where the rerun stands meanwhile.

A workflow still running is refused (exit 5): devctl ci jobs tells a slow
job from a stuck one. --cancel cancels it first, the recovery of a stuck
workflow, and reruns it once CircleCI reads it canceled. The rerun is
started, not waited for: devctl pr wait and devctl release wait wait for it.

A rerun and a cancel are writes: they take a CircleCI login that granted
Write access (devctl auth login --circleci-only, Write on the consent page);
a login that granted Read access only is exit 8 naming that login. GitHub is
not read.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, pipeline{id, number, url}, workflow{name,
id, status, url, createdAt, stoppedAt}, fromFailed, canceled, outcome
(rerun|refused), rerunId, rerunUrl, lastRerun{name, id, status, url,
createdAt, stoppedAt}. See docs/ci.md.

Exit codes:
  0  the rerun started; rerunId and rerunUrl are the new workflow
  2  --cancel: the workflow still reads running two minutes after the cancel
  3  not applicable: no such workflow, or one of another repository
  5  refused: a rerun of the workflow within the hour, a workflow still
     running without --cancel, or a rerun CircleCI refused (reason says why)
  7  usage or a tooling failure
  8  authentication required: no CircleCI login, or one without Write access`
	example = `  devctl ci rerun giantswarm/devctl f1290c29-9a4b-4e0e-8c6a-0b7d3e5f2a11 --from-failed
  devctl ci rerun giantswarm/devctl f1290c29-9a4b-4e0e-8c6a-0b7d3e5f2a11 --cancel`
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
		stdout:          config.Stdout,
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
	c.SetFlagErrorFunc(r.FlagError)

	f.Init(c)

	return c, nil
}
