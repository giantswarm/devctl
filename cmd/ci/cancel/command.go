package cancel

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "cancel <owner/repo> [workflow id]"
	description = "Cancel CircleCI workflows or queued pipelines made obsolete on GitHub."
	long        = `Cancel a workflow by ID, or all active workflows of a pipeline ID with
--pipeline. --stale scans every pipeline page and cancels queued workflows
whose branch head moved or whose PR closed. Tags and the default branch are
protected in stale mode. Running jobs and approval workflows are retained.
--dry-run reports candidates without writing. Requires the existing CircleCI
login with Write access; --stale also reads GitHub with the existing login.
Cancellation is asynchronous: cancel_requested means accepted, not finished.
Pipelines without workflows are reported as uncancelablePipelines because
CircleCI API v2 cancels workflows, not pipeline objects. See docs/ci.md.`
	example = `  devctl ci cancel giantswarm/devctl <workflow-id>
  devctl ci cancel giantswarm/devctl <pipeline-id> --pipeline
  devctl ci cancel giantswarm/devctl --stale --dry-run`
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
