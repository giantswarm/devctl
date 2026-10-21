package merge

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

const (
	statusLong = `Read a merge devctl pr merge --detach started, by the handle it printed: one
JSON document, an exit code.

The merge ended: the document carries the merge's own document in merge and
its exit code, verdict and reason, so the exit code reads as the blocking
devctl pr merge's would (0 merged and released, 6 and 9 merged, ...). The
merge runs: exit 10, verdict running, with its pid and log. The merge's
process is gone without a document (killed, the machine restarted): exit 7,
state lost; whether it merged is GitHub's to say, and devctl release wait
<owner/repo> --pr <n> confirms a release.

Without a handle, jobs lists every detached merge of this machine, the newest
first, with its state and exit code. Detached merges live in
$XDG_STATE_HOME/devctl/merges (~/.local/state/devctl/merges) and are removed
30 days after they started, once ended.

Exit codes: the merge's own (0-9) once it ended, 7 for a lost merge or an
unknown handle, 10 while it runs; 0 for the list.`
	statusExample = `  devctl pr merge status giantswarm-devctl-2278-20261007T091500Z
  devctl pr merge status`
)

type statusRunner struct {
	stdout  io.Writer
	jobRoot func() (string, error)
}

func newStatus(config Config) *cobra.Command {
	r := &statusRunner{stdout: config.Stdout, jobRoot: mergejob.Root}
	return &cobra.Command{
		Use:         "status [handle]",
		Short:       "Read the outcome of a merge devctl pr merge --detach started; one JSON document, an exit code.",
		Long:        statusLong,
		Example:     statusExample,
		Args:        cobra.ArbitraryArgs,
		Annotations: agentcli.AgentFacing(),
		RunE: func(_ *cobra.Command, args []string) error {
			return r.run(args)
		},
	}
}

func (r *statusRunner) run(args []string) error {
	doc := mergejob.NewStatusDocument(time.Now())
	ok, err := r.read(args, &doc)
	return agentcli.Report(r.stdout, &doc, ok, err)
}

// read fills doc and returns the verdict of an exit 0, or the outcome.
func (r *statusRunner) read(args []string, doc *mergejob.StatusDocument) (agentcli.Verdict, error) {
	if len(args) > 1 {
		return agentcli.VerdictGreen, fmt.Errorf("usage: devctl %s [handle], got %d arguments", mergejob.StatusCommand, len(args))
	}
	root, err := r.jobRoot()
	if err != nil {
		return agentcli.VerdictGreen, err
	}
	handle := ""
	if len(args) == 1 {
		handle = args[0]
	}
	return mergejob.ReadStatus(mergejob.Dir(root), handle, doc)
}
