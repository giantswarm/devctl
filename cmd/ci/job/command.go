package job

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "job <owner/repo> <job number>"
	description = "One CircleCI job: its status, its steps and the end of its failed steps' output; one JSON document, an exit code."
	long        = `Read one CircleCI job: its name, status, page and times, every step of it
(a parallel step once per action) with its status and times, and the last
lines of the output of its failed steps, led by the lines from its first
failure when that lies before them, without terminal control sequences.
That is why a job failed, read without the CircleCI UI.

The job is named by its number in the project: the number its page
(https://circleci.com/gh/<owner>/<repo>/<number>), a CircleCI commit status
and devctl ci jobs show. GitHub is not read. The read takes the CircleCI
login of devctl auth login, used in devctl's process only; a login that
granted Read access suffices. A step's output is read from its signed URL,
without the token.

The document (schemaVersion 1): command, exitCode, verdict, reason, warnings,
startedAt, finishedAt, repository, job{number, name, status, url, pipelineId,
workflowId, startedAt, stoppedAt, steps[{name, index, status, startedAt,
endedAt}], logTail, logError}. See docs/ci.md.

Exit codes:
  0  the job was read (verdict listed); logError says when its output was not
  3  not applicable: no such job of the repository
  7  usage or a tooling failure
  8  authentication required: no CircleCI login (devctl auth login --circleci-only)`
	example = `  devctl ci job giantswarm/devctl 7781
  devctl ci job giantswarm/devctl 7781 --lines 500 | jq -r .job.logTail`
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
