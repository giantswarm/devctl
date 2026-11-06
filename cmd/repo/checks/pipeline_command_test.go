package checks

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

func TestCommandPipelineRemovalBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		local string
	}{
		{name: "case 0: the remote pipeline does not authorize removal"},
		{name: "case 1: the explicit local pipeline authorizes CircleCI removal", local: "valid"},
		{name: "case 2: an empty local directory falls back safely", local: "empty"},
		{name: "case 3: malformed local YAML falls back safely", local: "malformed"},
		{name: "case 4: an unreadable local document falls back safely", local: "unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, update := range []bool{false, true} {
				name := "check"
				if update {
					name = "update"
				}
				t.Run(name, func(t *testing.T) {
					f := newChecksFixture(t)
					f.protection = nil
					f.ruleset = fixtureRuleset(
						&github.RuleStatusCheck{Context: "ci/circleci: obsolete"},
						&github.RuleStatusCheck{Context: "Heimdall - PR Gatekeeper", IntegrationID: new(int64(4321))},
					)
					f.reported = []string{"ci/circleci: build"}
					args := []string{"--output=json"}
					if update {
						args = append(args, "--update")
					}
					if tc.local != "" {
						dir := t.TempDir()
						path := filepath.Join(dir, "workflows.yml")
						switch tc.local {
						case "valid":
							require.NoError(t, os.WriteFile(path, []byte("workflows:\n  build:\n    jobs: [build]\n"), 0o600))
						case "malformed":
							require.NoError(t, os.WriteFile(path, []byte("workflows: [oops"), 0o600))
						case "unreadable":
							// A directory read fails even when the tests run as root.
							require.NoError(t, os.Mkdir(path, 0o700))
						}
						args = append(args, "--circleci-dir="+dir)
					}
					output, err := f.execute(t.Context(), append(args, "fixture/repository")...)
					require.NoError(t, err)
					var result reconcile.Result
					require.NoError(t, json.Unmarshal([]byte(output), &result))
					require.Equal(t, "fixture/repository", result.Repository)
					require.Len(t, result.Steps, 1)
					if !update {
						require.Equal(t, reconcile.ModeCheck, result.Mode)
						require.Equal(t, reconcile.VerdictDrift, result.Step(reconcile.StepProtection).Verdict)
						require.Empty(t, f.writes)
						require.Equal(t, []string{"ci/circleci: obsolete", "Heimdall - PR Gatekeeper"}, fixtureCheckNames(f.ruleset))
						return
					}
					require.Equal(t, reconcile.ModeRepair, result.Mode)
					require.Equal(t, reconcile.VerdictRepaired, result.Step(reconcile.StepProtection).Verdict)
					require.Len(t, f.writes, 1)
					require.Equal(t, http.MethodPut, f.writes[0].method)
					require.Equal(t, "/rulesets/7", f.writes[0].path)
					want := []string{"Heimdall - PR Gatekeeper", "ci/circleci: build"}
					if tc.local != "valid" {
						want = append(want, "ci/circleci: obsolete")
					}
					require.ElementsMatch(t, want, fixtureCheckNames(f.ruleset))
				})
			}
		})
	}
}

func TestCommandRemoveExactContext(t *testing.T) {
	for _, output := range []string{"json", "table"} {
		t.Run(output, func(t *testing.T) {
			f := newChecksFixture(t)
			f.protection = nil
			f.ruleset = fixtureRuleset(&github.RuleStatusCheck{Context: "external.gate"}, &github.RuleStatusCheck{Context: "externalXgate"})
			text, err := f.execute(t.Context(), "--update", "--remove=external.gate", "--output="+output, "fixture/repository")
			require.NoError(t, err)
			require.Contains(t, text, "stop requiring external.gate")
			require.Equal(t, []string{"externalXgate"}, fixtureCheckNames(f.ruleset))
			require.Len(t, f.writes, 1)
		})
	}
}

func TestCommandValidationBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "case 0: missing repository"},
		{name: "case 1: extra repository", args: []string{"fixture/repository", "fixture/second"}},
		{name: "case 2: missing owner", args: []string{"repository"}},
		{name: "case 3: unsupported output", args: []string{"--output=yaml", "fixture/repository"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChecksFixture(t)
			_, err := f.execute(t.Context(), tc.args...)
			require.Error(t, err)
			require.Empty(t, f.requests)
			require.Empty(t, f.writes)
		})
	}
}

func fixtureCheckNames(ruleset *github.RepositoryRuleset) []string {
	var names []string
	for _, check := range ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks {
		names = append(names, check.Context)
	}
	return names
}
