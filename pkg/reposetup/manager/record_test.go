package manager

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// A record the manager answered for a refused entry decodes: its
// declaration carries the schema's refusals as `field: message` strings
// (testdata/record-refused.json is get_repository's answer for
// giantswarm/agent-platform, the last run left out).
func TestRecordRefused(t *testing.T) {
	raw, err := os.ReadFile("testdata/record-refused.json")
	if err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("decode: %v", err)
	}

	d := r.Declaration
	if d == nil || d.Accepted || d.Team != "team-bumblebee" {
		t.Fatalf("declaration: %+v", d)
	}
	want := "gen.ci.chartReleaseGateJob: not a field of the repositories schema"
	if len(d.Problems) != 1 || d.Problems[0] != want {
		t.Errorf("problems: %q, want [%q]", d.Problems, want)
	}
	if len(r.Findings) != 1 || r.Findings[0].Kind != string(reconcile.FindingEntryRefused) || r.Findings[0].Message != want {
		t.Errorf("findings: %+v", r.Findings)
	}
	if c := r.Setup.Checks; c == nil || c.Converged || len(c.Steps) != 1 || c.Steps[0].Step != reconcile.StepEntry {
		t.Errorf("checks: %+v", c)
	}
}
