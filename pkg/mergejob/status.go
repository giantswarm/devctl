package mergejob

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// StatusCommand is the command of a status document.
const StatusCommand = "pr merge status"

// Jobs is where the merges a status reads are kept.
type Jobs interface {
	// List returns every merge, the newest first.
	List() ([]Status, error)
	// Load returns the merge of handle, an error for an unknown one.
	Load(handle string) (Status, error)
}

// Dir is the job directories under a root: the detached merges of a laptop.
type Dir string

func (d Dir) List() ([]Status, error)            { return List(string(d)) }
func (d Dir) Load(handle string) (Status, error) { return Load(string(d), handle) }

// Memory is the merges a long-running process keeps for as long as it runs:
// the server's, which writes nothing to disk.
type Memory struct {
	mu   sync.Mutex
	jobs map[string]Status
}

// Put records s under its handle, replacing an earlier state.
func (m *Memory) Put(s Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.jobs == nil {
		m.jobs = map[string]Status{}
	}
	m.jobs[s.Handle] = s
}

func (m *Memory) List() ([]Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := make([]Status, 0, len(m.jobs))
	for _, s := range m.jobs {
		jobs = append(jobs, s)
	}
	sort.SliceStable(jobs, func(a, b int) bool { return jobs[a].StartedAt.After(jobs[b].StartedAt) })
	return jobs, nil
}

func (m *Memory) Load(handle string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.jobs[handle]
	if !ok {
		return Status{}, fmt.Errorf("no merge %s on this server", handle)
	}
	return s, nil
}

// StatusDocument is the document of devctl pr merge status.
type StatusDocument struct {
	agentcli.Envelope
	Handle     string          `json:"handle,omitempty"`
	State      string          `json:"state,omitempty"`
	Repository string          `json:"repository,omitempty"`
	Number     int             `json:"number,omitempty"`
	PID        int             `json:"pid,omitempty"`
	Log        string          `json:"log,omitempty"`
	Merge      json.RawMessage `json:"merge"`
	OnDone     *OnDone         `json:"onDone"`
	Jobs       []JobSummary    `json:"jobs,omitempty"`
}

// JobSummary is one merge in the list.
type JobSummary struct {
	Handle     string    `json:"handle"`
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	State      string    `json:"state"`
	StartedAt  time.Time `json:"startedAt"`
	ExitCode   *int      `json:"exitCode"`
}

// NewStatusDocument is the document of a status read started at now.
func NewStatusDocument(now time.Time) StatusDocument {
	return StatusDocument{Envelope: agentcli.NewEnvelope(StatusCommand, now), Merge: json.RawMessage("null")}
}

// ReadStatus fills doc with the merge of handle, or the list of every merge
// without one, and returns the verdict of an exit 0, or the outcome: the
// merge's own once it ended, running while it runs, usage for a lost merge
// or an unknown handle.
func ReadStatus(jobs Jobs, handle string, doc *StatusDocument) (agentcli.Verdict, error) {
	if handle == "" {
		list, err := jobs.List()
		if err != nil {
			return agentcli.VerdictGreen, err
		}
		doc.Jobs = make([]JobSummary, 0, len(list))
		for _, s := range list {
			j := JobSummary{Handle: s.Handle, Repository: s.Repository, Number: s.Number, State: s.State, StartedAt: s.StartedAt}
			if s.State == StateFinished {
				if env, err := envelopeOf(s.Document); err == nil {
					j.ExitCode = &env.ExitCode
				}
			}
			doc.Jobs = append(doc.Jobs, j)
		}
		return agentcli.VerdictGreen, nil
	}
	s, err := jobs.Load(handle)
	if err != nil {
		return agentcli.VerdictGreen, err
	}
	doc.Handle, doc.State, doc.Repository, doc.Number, doc.PID, doc.Log, doc.OnDone = s.Handle, s.State, s.Repository, s.Number, s.PID, s.Log(), s.OnDone
	switch s.State {
	case StateRunning:
		return agentcli.VerdictGreen, agentcli.NewExitError(agentcli.ExitRunning, agentcli.VerdictRunning,
			"%s#%d still merges%s", s.Repository, s.Number, s.whereItRuns())
	case StateLost:
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

// whereItRuns names a running merge's process and log, when it has them.
func (s Status) whereItRuns() string {
	if s.Dir == "" {
		return ""
	}
	return fmt.Sprintf(" in pid %d; its progress is in %s", s.PID, s.Log())
}

func envelopeOf(document []byte) (agentcli.Envelope, error) {
	var env agentcli.Envelope
	err := json.Unmarshal(document, &env)
	return env, err
}
