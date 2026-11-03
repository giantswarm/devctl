package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Test_AutoReleaseStableGateOrder pins where the repository's stable gate runs:
// on a stable run alone, after the decide step named the tag and before the
// step that creates the tag and the release, so a refusal cuts neither. Every
// flavour carries it.
func Test_AutoReleaseStableGateOrder(t *testing.T) {
	for _, flavour := range []gen.Flavour{gen.FlavourApp, gen.FlavourFork} {
		t.Run(string(flavour), func(t *testing.T) {
			steps, rendered := tagJobStepsFor(t, flavour)
			ids := make([]string, 0, len(steps))
			for _, s := range steps {
				ids = append(ids, s.ID)
			}

			decide, gate, release := slices.Index(ids, "decide"), slices.Index(ids, "gate"), slices.Index(ids, "release")
			if gate < 0 {
				t.Fatalf("no step with id \"gate\" in the tag job:\n%s", rendered)
			}
			if gate < decide || gate > release {
				t.Errorf("step order %v: the gate has to run after decide and before release", ids)
			}
			if want := "steps.decide.outputs.prerelease == 'false' && steps.decide.outputs.tag != ''"; steps[gate].If != want {
				t.Errorf("gate if = %q, want %q", steps[gate].If, want)
			}
		})
	}
}

// Test_AutoReleaseStableGate pins what the gate does with the candidate's
// checkout: no script is no gate, a script that passes lets the release go
// on, and a script that refuses fails the step, so the release step never
// runs. The script gets the tag about to be cut.
func Test_AutoReleaseStableGate(t *testing.T) {
	gate := tagJobStep(t, "gate").Run

	testCases := []struct {
		name      string
		script    string
		expectErr bool
		expectLog string
	}{
		{
			name:      "a repository without a gate is released",
			expectLog: "no .github/release-gate.custom.sh on the released commit",
		},
		{
			name:      "a gate that passes lets the release go on",
			script:    "echo \"checked $1\"\n",
			expectLog: "checked v1.3.0",
		},
		{
			name:      "a gate that refuses stops the release",
			script:    "echo \"$1 has no ledger row\" >&2\nexit 1\n",
			expectErr: true,
			expectLog: "::error title=Stable gate refused::.github/release-gate.custom.sh refused v1.3.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.script != "" {
				if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o750); err != nil {
					t.Fatalf("mkdir .github: %v", err)
				}
				// Not executable: the step runs it with bash, so a repository
				// that loses the mode bit keeps its gate.
				if err := os.WriteFile(filepath.Join(dir, ".github", "release-gate.custom.sh"), []byte(tc.script), 0o600); err != nil {
					t.Fatalf("write gate: %v", err)
				}
			}

			_, log, err := runStep(t, gate, dir, "TAG=v1.3.0")
			if tc.expectErr != (err != nil) {
				t.Fatalf("err = %v, want error %v\n%s", err, tc.expectErr, log)
			}
			if !strings.Contains(log, tc.expectLog) {
				t.Errorf("log does not contain %q:\n%s", tc.expectLog, log)
			}
		})
	}
}
