package merge

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

const (
	statusCommand = "pr merge status"
	statusLong    = `Read a merge devctl pr merge --detach started, by the handle it printed: one
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

// statusDocument is the document of devctl pr merge status.
type statusDocument struct {
	agentcli.Envelope
	Handle     string           `json:"handle,omitempty"`
	State      string           `json:"state,omitempty"`
	Repository string           `json:"repository,omitempty"`
	Number     int              `json:"number,omitempty"`
	PID        int              `json:"pid,omitempty"`
	Log        string           `json:"log,omitempty"`
	Merge      json.RawMessage  `json:"merge"`
	OnDone     *mergejob.OnDone `json:"onDone"`
	Jobs       []jobSummary     `json:"jobs,omitempty"`
}

// jobSummary is one detached merge in the list.
type jobSummary struct {
	Handle     string    `json:"handle"`
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	State      string    `json:"state"`
	StartedAt  time.Time `json:"startedAt"`
	ExitCode   *int      `json:"exitCode"`
}

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
	doc := statusDocument{Envelope: agentcli.NewEnvelope(statusCommand, time.Now()), Merge: json.RawMessage("null")}
	ok, err := r.read(args, &doc)
	return agentcli.Report(r.stdout, &doc, ok, err)
}

// read fills doc and returns the verdict of an exit 0, or the outcome.
func (r *statusRunner) read(args []string, doc *statusDocument) (agentcli.Verdict, error) {
	root, err := r.jobRoot()
	if err != nil {
		return agentcli.VerdictGreen, err
	}
	switch len(args) {
	case 0:
		jobs, err := mergejob.List(root)
		if err != nil {
			return agentcli.VerdictGreen, err
		}
		doc.Jobs = make([]jobSummary, 0, len(jobs))
		for _, s := range jobs {
			j := jobSummary{Handle: s.Handle, Repository: s.Repository, Number: s.Number, State: s.State, StartedAt: s.StartedAt}
			if s.State == mergejob.StateFinished {
				if env, err := envelopeOf(s.Document); err == nil {
					j.ExitCode = &env.ExitCode
				}
			}
			doc.Jobs = append(doc.Jobs, j)
		}
		return agentcli.VerdictGreen, nil
	case 1:
	default:
		return agentcli.VerdictGreen, fmt.Errorf("usage: devctl %s [handle], got %d arguments", statusCommand, len(args))
	}
	s, err := mergejob.Load(root, args[0])
	if err != nil {
		return agentcli.VerdictGreen, err
	}
	doc.Handle, doc.State, doc.Repository, doc.Number, doc.PID, doc.Log, doc.OnDone = s.Handle, s.State, s.Repository, s.Number, s.PID, s.Log(), s.OnDone
	switch s.State {
	case mergejob.StateRunning:
		return agentcli.VerdictGreen, agentcli.NewExitError(agentcli.ExitRunning, agentcli.VerdictRunning,
			"%s#%d still merges in pid %d; its progress is in %s", s.Repository, s.Number, s.PID, s.Log())
	case mergejob.StateLost:
		return agentcli.VerdictGreen, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage,
			"the detached merge of %s#%d (pid %d) ended without its document, see %s: check the pull request before merging again; devctl release wait %s --pr %d confirms a release",
			s.Repository, s.Number, s.PID, s.Log(), s.Repository, s.Number)
	}
	doc.Merge = s.Document
	env, err := envelopeOf(s.Document)
	if err != nil {
		return agentcli.VerdictGreen, fmt.Errorf("reading the document of %s: %w", s.Handle, err)
	}
	if env.ExitCode == agentcli.ExitOK {
		return env.Verdict, nil
	}
	return agentcli.VerdictGreen, &agentcli.ExitError{Code: env.ExitCode, Verdict: env.Verdict, Reason: env.Reason}
}

func envelopeOf(document []byte) (agentcli.Envelope, error) {
	var env agentcli.Envelope
	err := json.Unmarshal(document, &env)
	return env, err
}
