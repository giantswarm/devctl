package status

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

func result() *reconcile.Result {
	return &reconcile.Result{
		Repository: "giantswarm/my-service",
		Declared:   "giantswarm/my-service",
		Team:       "team-bumblebee",
		Mode:       reconcile.ModeCheck,
		Converged:  true,
	}
}

// fakeCaller answers every tool with one payload.
type fakeCaller struct {
	payload string
	tool    string
	args    map[string]any
}

func (f *fakeCaller) Call(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	f.tool, f.args = tool, args
	return json.RawMessage(f.payload), nil
}

func newRunner(payload string, output string) (*runner, *fakeCaller, *bytes.Buffer) {
	caller := &fakeCaller{payload: payload}
	var out bytes.Buffer
	r := &runner{
		flag:   &flag{Flags: client.Flags{Output: output}},
		logger: logrus.New(),
		stdout: &out,
		stderr: &bytes.Buffer{},
		open: func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: caller, Endpoint: "https://muster.example/mcp"}, nil
		},
	}
	return r, caller, &out
}

const record = `{
  "repository": "giantswarm/my-service",
  "declaration": {"team": "team-bumblebee", "file": "repositories/team-bumblebee.yaml", "componentType": "service",
    "entry": "- name: my-service\n  componentType: service\n  align: true\n  gen:\n    flavours: [app]\n    language: go\n", "accepted": true},
  "reality": {"url": "https://github.com/giantswarm/my-service"},
  "setup": {
    "checks": {"repository": "giantswarm/my-service", "declared": "giantswarm/my-service", "team": "team-bumblebee", "mode": "check", "converged": true,
      "steps": [{"step": "settings", "verdict": "ok", "summary": "as the baseline"}]},
    "checkedAt": "2026-09-22T02:20:00Z",
    "lastRun": {"result": {"repository": "giantswarm/my-service"}, "runUrl": "https://github.com/giantswarm/github/actions/runs/1", "timestamp": "2026-09-21T02:20:00Z",
      "change": {"kind": "nightly"}},
    "pendingRun": {"dispatchedAt": "2026-09-22T10:00:00Z", "by": "alice", "kind": "archived", "pullRequest": {"number": 6179, "url": "https://github.com/giantswarm/github/pull/6179"}}
  },
  "findings": [{"kind": "reconcile-run-missing", "message": "the run never reported", "fix": "open the Actions page", "source": "inventory"},
               {"kind": "default-icon", "message": "the default icon", "fix": "replace it", "source": "engine"}],
  "source": "sweep", "age": "5m"
}`

// The text output reads the record: the declaration's alignment lines from
// the entry, the steps, the verdict, the runs and the inventory's own
// findings (the engine's are in the steps already); the manager is named
// as the source.
func TestRunText(t *testing.T) {
	r, caller, out := newRunner(record, client.OutputText)
	require.NoError(t, r.run(context.Background(), "my-service"))
	require.Equal(t, manager.ToolGetRepository, caller.tool)
	require.Equal(t, "my-service", caller.args["repository"])

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Equal(t, "giantswarm/my-service declared in team-bumblebee (check mode, from giantswarm-repo-manager at https://muster.example/mcp, checked 2026-09-22T02:20:00Z)", lines[0])
	require.Equal(t, "opted in to alignment (align: true): the reconciler changes this repository to its declared set-up on every trigger", lines[1])
	require.Equal(t, "default branch main, flavours app", lines[2])
	require.Equal(t, "  settings     ok        as the baseline", lines[3])
	require.Equal(t, "converged: set up as declared", lines[4])
	require.Equal(t, "last run: https://github.com/giantswarm/github/actions/runs/1 at 2026-09-21T02:20:00Z (nightly)", lines[5])
	require.Equal(t, "pending run: expected since 2026-09-22T10:00:00Z by alice (archived, https://github.com/giantswarm/github/pull/6179)", lines[6])
	require.Equal(t, "findings:", lines[7])
	require.Equal(t, "  reconcile-run-missing: the run never reported -- fix: open the Actions page (inventory)", lines[8])
	require.NotContains(t, out.String(), "default-icon")
}

// The JSON output is the manager's record as it came.
func TestRunJSON(t *testing.T) {
	r, _, out := newRunner(record, client.OutputJSON)
	require.NoError(t, r.run(context.Background(), "giantswarm/my-service"))
	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, "giantswarm/my-service", got["repository"])
	require.Contains(t, got, "declaration")
	require.Contains(t, got, "setup")
}

// An undeclared repository and one the inventory has not checked are
// errors naming what to do; nothing runs locally.
func TestRunRefusals(t *testing.T) {
	r, _, _ := newRunner(`{"repository": "giantswarm/orphan", "declaration": null, "reality": {"url": "x"}, "setup": {}}`, client.OutputText)
	err := r.run(context.Background(), "orphan")
	require.True(t, IsNotDeclared(err), err)
	require.Contains(t, err.Error(), "devctl repo adopt")

	r, _, _ = newRunner(`{"repository": "giantswarm/fresh", "declaration": {"team": "team-bumblebee", "file": "f"}, "setup": {"checkError": "no read identity"}}`, client.OutputText)
	err = r.run(context.Background(), "fresh")
	require.True(t, IsNoSetupState(err), err)
	require.Contains(t, err.Error(), "no read identity")
}

// The text output names the repository's opt-in to alignment right under
// the declaration's line when the record carries the entry, and says nothing
// about it when it does not.
func TestPrintAlign(t *testing.T) {
	optedIn, notOptedIn := true, false
	cases := []struct {
		name  string
		align *bool
		want  string
	}{
		{name: "opted in", align: &optedIn, want: "opted in to alignment (align: true): the reconciler changes this repository to its declared set-up on every trigger"},
		{name: "not opted in", align: &notOptedIn, want: "not opted in to alignment: the reconciler checks this repository and changes nothing; opt in with align: true in its entry"},
		{name: "unknown", align: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			r := &runner{flag: &flag{}, stdout: &out}
			require.NoError(t, r.print(&output{Result: result(), Align: tc.align}))

			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			require.Equal(t, "giantswarm/my-service declared in team-bumblebee (check mode, from giantswarm-repo-manager)", lines[0])
			if tc.want == "" {
				require.Equal(t, "converged: set up as declared", lines[1])
				require.NotContains(t, out.String(), "alignment")
				return
			}
			require.Equal(t, tc.want, lines[1])
		})
	}
}

// The text output marks an advisory finding and, when a finding a person
// must fix remains, says why the run did not converge.
func TestPrintFindings(t *testing.T) {
	res := result()
	res.Converged = false
	res.Steps = []reconcile.StepResult{{Step: reconcile.StepScaffold, Verdict: reconcile.VerdictReported, Summary: "present", Findings: []reconcile.Finding{
		{Kind: reconcile.FindingDefaultIcon, Message: "the default icon", Fix: "replace it", Advisory: true},
		{Kind: reconcile.FindingABSPrerequisite, Message: "no schema", Fix: "add it"},
	}}}
	var out bytes.Buffer
	r := &runner{flag: &flag{}, stdout: &out}
	require.NoError(t, r.print(&output{Result: res}))
	require.Contains(t, out.String(), "default-icon (advisory): the default icon -- fix: replace it")
	require.Contains(t, out.String(), "abs-prerequisite: no schema -- fix: add it")
	require.True(t, strings.HasSuffix(out.String(), "not converged: drift, failed steps or findings to fix above\n"), out.String())
}

// The text output of a refused entry says the declaration is at fault and
// nothing was checked, never that something drifted.
func TestPrintRefused(t *testing.T) {
	res := result()
	res.Converged = false
	res.Steps = []reconcile.StepResult{{Step: reconcile.StepEntry, Verdict: reconcile.VerdictReported, Summary: "refused: 1 problem(s) with the entry in repositories/team-bumblebee.yaml", Findings: []reconcile.Finding{
		{Kind: reconcile.FindingEntryRefused, Message: "agentMerge: not a field of the repositories schema", Fix: `edit agentMerge of the entry "my-service" in repositories/team-bumblebee.yaml: not a field of the repositories schema`},
	}}}
	var out bytes.Buffer
	r := &runner{flag: &flag{}, stdout: &out}
	require.NoError(t, r.print(&output{Endpoint: "https://muster.example", Result: res}))
	require.Contains(t, out.String(), "  entry        reported  refused: 1 problem(s)")
	require.Contains(t, out.String(), "entry-refused: agentMerge: not a field of the repositories schema -- fix: edit agentMerge")
	require.True(t, strings.HasSuffix(out.String(), "not converged: the entry is refused and nothing was checked; fix the declaration as the findings above say\n"), out.String())
	require.NotContains(t, out.String(), "drift")
}

// A missing run is named with where to look.
func TestPrintMissingRun(t *testing.T) {
	var out bytes.Buffer
	r := &runner{flag: &flag{}, stdout: &out}
	at := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	require.NoError(t, r.print(&output{Result: result(), MissingRun: &manager.MissingRun{DispatchedAt: at, By: "alice", RunsURL: "https://github.com/giantswarm/github/actions/workflows/reconcile-repositories.yaml"}}))
	require.Contains(t, out.String(), "missing run: the run expected since 2026-09-22T10:00:00Z by alice never reported: https://github.com/giantswarm/github/actions/workflows/reconcile-repositories.yaml")
}
