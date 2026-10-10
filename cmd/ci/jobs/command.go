package jobs

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "jobs <owner/repo> <pipeline number|id>"
	description = "The jobs of a CircleCI pipeline's workflows, and where a running one stands; one JSON document, an exit code."
	long        = `Read a CircleCI pipeline at the job level: every workflow run of it, reruns
included (the newest run of a name is marked latest), with its jobs, each with
its status, number, when it started and stopped and how long it ran or has
been running. A running job also carries the step it is in: when the step
started, when it last wrote a line of output and how long ago that was. A
slow job writes output seconds ago; a stuck one wrote an hour ago, or never.
That is what tells the two apart while a workflow reads running.

The pipeline is named by its number in the project (the number the CircleCI
UI and devctl pr wait show) or by its id. GitHub is not read. The view takes
the CircleCI login of devctl auth login; a login that granted Read access
suffices.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, pipeline{id, number, state, url,
createdAt, branch, tag, revision}, workflows[{name, id, status, url,
createdAt, stoppedAt, latest, jobs[{name, number, type, status, startedAt,
stoppedAt, durationSeconds, step{name, startedAt, endedAt, runningSeconds,
lastOutputAt, outputAgeSeconds}}]}]. See docs/ci.md.

Exit codes:
  0  the pipeline was read (verdict listed); the document says where it stands
  3  not applicable: no such pipeline of the repository
  7  usage or a tooling failure
  8  authentication required: no CircleCI login (devctl auth login --circleci-only)`
	example = `  devctl ci jobs giantswarm/devctl 3885
  devctl ci jobs giantswarm/devctl b311160a-5f0c-4a21-9d7e-2c0f6e1a9b3d`
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

	return c, nil
}
