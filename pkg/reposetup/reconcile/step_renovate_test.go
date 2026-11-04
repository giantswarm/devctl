package reconcile

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRenovateFork exercises the GitHub fork flag rather than the entry's
// flavour, through the real repository and file readers in both modes.
func TestRenovateFork(t *testing.T) {
	cases := []struct {
		name         string
		files        map[string]string
		fork         bool
		young        bool
		dashboard    bool
		issuesStatus int
		declaredOff  bool
		wantFork     bool
		wantFinding  FindingKind
		wantSummary  string
	}{
		{
			name: "case 0: JSON5 alone fails the fork check", fork: true, wantFork: true,
			files: map[string]string{"renovate.json5": "{}"},
		},
		{
			name: "case 1: JSON5 cannot admit a fork", fork: true, wantFork: true,
			files: map[string]string{"renovate.json5": `{forkProcessing: 'enabled'}`},
		},
		{
			name: "case 2: JSON without forkProcessing fails the fork check", fork: true, wantFork: true,
			files: map[string]string{"renovate.json": `{extends: ['github>example/preset']}`},
		},
		{
			name: "case 3: JSONC from the fork generator is judged by its trace", fork: true, dashboard: true,
			files:       map[string]string{"renovate.json": "{\n  // Generated configuration\n  \"forkProcessing\": \"enabled\"\n}\n"},
			wantSummary: "renovate.json; Dependency Dashboard issue #1",
		},
		{
			name: "case 4: an admitted fork still needs a trace", fork: true,
			files:       map[string]string{"renovate.json": `{"forkProcessing":"enabled"}`},
			wantFinding: FindingRenovateNotScanned,
		},
		{
			name: "case 5: an admitted young fork owes no trace", fork: true, young: true,
			files:       map[string]string{"renovate.json": `{"forkProcessing":"enabled"}`},
			wantSummary: "renovate.json; no Renovate run yet and none due: the repository is younger than a day, Renovate's first run follows",
		},
		{
			name: "case 6: a JSON5 opt-out stays disabled", fork: true,
			files:       map[string]string{"renovate.json5": "{\n  enabled: false,\n}\n"},
			wantSummary: "renovate.json5 disables Renovate",
		},
		{
			name: "case 7: a JSON opt-out stays disabled", fork: true,
			files:       map[string]string{"renovate.json": "{\n  \"enabled\": false\n}\n"},
			wantSummary: "renovate.json disables Renovate",
		},
		{
			name: "case 8: a skipped young fork is reported now", fork: true, young: true, wantFork: true,
			files: map[string]string{"renovate.json5": "{}"},
		},
		{
			name: "case 9: historical evidence cannot admit a fork", fork: true, dashboard: true, wantFork: true,
			files: map[string]string{"renovate.json5": "{}"},
		},
		{
			name: "case 10: missing issues permission cannot hide the fork check", fork: true, issuesStatus: 403, wantFork: true,
			files: map[string]string{"renovate.json5": "{}"},
		},
		{
			name: "case 11: no configuration on a fork names the fork generator", fork: true, wantFork: true,
		},
		{
			name: "case 12: a non-fork needs no admission", dashboard: true,
			files:       map[string]string{"renovate.json5": "{}"},
			wantSummary: "renovate.json5; Dependency Dashboard issue #1",
		},
		{
			name: "case 13: a nested key cannot admit a fork", fork: true, wantFork: true,
			files: map[string]string{"renovate.json": `{"packageRules":[{"forkProcessing":"enabled"}]}`},
		},
		{
			name: "case 14: a comment cannot admit a fork", fork: true, wantFork: true,
			files: map[string]string{"renovate.json": "{\n // \"forkProcessing\": \"enabled\"\n}"},
		},
		{
			name: "case 15: invalid config cannot admit a fork", fork: true, wantFork: true,
			files: map[string]string{"renovate.json": `{"forkProcessing":"enabled"`},
		},
		{
			name: "case 16: legacy includeForks still admits a fork", fork: true, dashboard: true,
			files:       map[string]string{"renovate.json": `{"includeForks":true}`},
			wantSummary: "renovate.json; Dependency Dashboard issue #1",
		},
		{
			name: "case 17: JSON5 fallback matches Renovate", fork: true, dashboard: true,
			files:       map[string]string{"renovate.json": `{forkProcessing: 'enabled',}`},
			wantSummary: "renovate.json; Dependency Dashboard issue #1",
		},
		{
			name: "case 18: admission in JSONC has the wrong filename", fork: true, wantFork: true,
			files: map[string]string{"renovate.jsonc": `{"forkProcessing":"enabled"}`},
		},
		{
			name: "case 19: a lower-priority opt-out does not hide the fork check", fork: true, wantFork: true,
			files: map[string]string{"renovate.json": "{}", "renovate.json5": "{\n  enabled: false,\n}\n"},
		},
		{
			name: "case 20: declared-off policy still takes precedence", fork: true, declaredOff: true,
			files:       map[string]string{"renovate.json5": "{}"},
			wantFinding: FindingRenovateDeclaredOff,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := entryYAML
			if tc.declaredOff {
				entry = renovateFalseEntryYAML
			}
			h := newHarness(t, entry)
			repo := h.gh.addRepo(owner, name)
			repo.fork, repo.files, repo.issuesStatus = tc.fork, tc.files, tc.issuesStatus
			repo.defaultBranch = "giantswarm"
			if tc.young {
				repo.createdAt = time.Now().Add(-time.Hour)
			}
			if tc.dashboard {
				repo.addIssue(renovateLogin, renovateDashboardTitle, false)
			}
			for _, mode := range []Mode{ModeCheck, ModeRepair} {
				t.Run(string(mode), func(t *testing.T) {
					res := h.run(mode, false, StepRenovate)
					sr := res.Step(StepRenovate)
					wantFinding := tc.wantFinding
					if tc.wantFork {
						wantFinding = FindingRenovateNotScanned
					}
					if wantFinding != "" {
						require.Equal(t, VerdictReported, sr.Verdict, "%+v", sr)
						require.Equal(t, []FindingKind{wantFinding}, kinds(sr.Findings))
						require.False(t, res.Converged)
						require.Equal(t, tc.wantFork, strings.Contains(sr.Findings[0].Message, "Renovate's fork check"))
						if tc.wantFork {
							require.Contains(t, sr.Findings[0].Fix, "devctl gen renovate --flavour fork")
							require.Contains(t, sr.Findings[0].Fix, `forkProcessing: "enabled"`)
							require.Contains(t, sr.Findings[0].Fix, "on giantswarm")
							require.Zero(t, h.gh.reads("/repos/"+owner+"/"+name+"/issues"))
							require.Zero(t, h.gh.reads("/repos/"+owner+"/"+name+"/commits"))
							require.Zero(t, h.gh.reads(fmt.Sprintf("/user/installations/%d/repositories", h.baseline.RenovateInstallationID)))
						} else if wantFinding == FindingRenovateNotScanned {
							require.Contains(t, sr.Findings[0].Message, "no trace of a Renovate run")
						}
					} else {
						require.Equal(t, VerdictOK, sr.Verdict, "%+v", sr)
						require.Equal(t, tc.wantSummary, sr.Summary)
						require.Empty(t, sr.Findings)
						require.True(t, res.Converged)
					}
					require.Empty(t, h.mutations(), "Renovate findings are never repaired automatically")
				})
			}
		})
	}
}

// TestRenovateConfigPrecedence gives each candidate a conflicting opt-out
// below it. Selecting that lower-priority file would hide a missing run.
func TestRenovateConfigPrecedence(t *testing.T) {
	paths := []string{
		"renovate.json", "renovate.jsonc", "renovate.json5",
		".github/renovate.json", ".github/renovate.jsonc", ".github/renovate.json5",
		".renovaterc", ".renovaterc.json", ".renovaterc.jsonc", ".renovaterc.json5",
	}
	for i, path := range paths {
		t.Run("case "+strconv.Itoa(i)+": "+path, func(t *testing.T) {
			h := newHarness(t, entryYAML)
			repo := h.gh.addRepo(owner, name)
			repo.files = map[string]string{path: "{}"}
			for _, lower := range paths[i+1:] {
				repo.files[lower] = "{\n  \"enabled\": false\n}\n"
			}
			res := h.run(ModeCheck, false, StepRenovate)
			sr := res.Step(StepRenovate)
			require.Equal(t, VerdictReported, sr.Verdict, "%+v", sr)
			require.Equal(t, []FindingKind{FindingRenovateNotScanned}, kinds(sr.Findings))
			require.Contains(t, sr.Findings[0].Message, "has "+path+" but no trace")
			var probes []string
			prefix := "/repos/" + owner + "/" + name + "/contents/"
			for _, get := range h.gets() {
				if strings.HasPrefix(get, prefix) {
					probes = append(probes, strings.TrimPrefix(get, prefix))
				}
			}
			require.Equal(t, paths[:i+1], probes, "stop probing at the effective config; never reread it")
			require.Empty(t, h.mutations())
		})
	}
}
