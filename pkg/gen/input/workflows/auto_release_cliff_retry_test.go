package workflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Exercise the generated step with offline failures like git-cliff's metadata
// panics. Fake sleep records backoff without delaying the tests.
func Test_AutoReleaseCliffRetry(t *testing.T) {
	const context = `[{"version":"v1.2.3","commits":[{"id":"abc"}]}]`
	for _, workflow := range []struct {
		name        string
		script      string
		bump        string
		wantArgs    string
		maintenance bool
	}{
		{name: "ordinary", script: tagJobStep(t, "cliff").Run, wantArgs: "--unreleased --bump --context"},
		{name: "maintenance default branch", script: maintenanceStep(t, "cliff").Run, wantArgs: "--unreleased --bump auto --context", maintenance: true},
		{name: "maintenance patch branch", script: maintenanceStep(t, "cliff").Run, bump: "patch", wantArgs: "--unreleased --bump patch --context", maintenance: true},
	} {
		t.Run(workflow.name, func(t *testing.T) {
			for _, tc := range []struct {
				name         string
				failures     int
				cause        string
				context      string
				emptyCommits bool
			}{
				{name: "first attempt succeeds", context: context},
				{name: "truncated body then success", failures: 1, cause: "Could not get github metadata: end of file before message length reached", context: context},
				{name: "timeouts then final attempt succeeds", failures: 2, cause: "Could not get github metadata: operation timed out", context: context},
				{name: "HTTP 500 exhausts retries", failures: 3, cause: "Could not get github metadata: HttpClientError(Status(500))", context: context},
				{name: "empty commits after retry", failures: 1, cause: "Could not get github metadata: operation timed out", context: `[{"version":"v1.2.3","commits":[]}]`, emptyCommits: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					dir := t.TempDir()
					bin := filepath.Join(dir, "bin")
					if err := os.Mkdir(bin, 0o700); err != nil {
						t.Fatal(err)
					}
					for name, script := range map[string]string{
						"git-cliff": `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> cliff-calls
attempt=$(wc -l < cliff-calls)
if [ "$attempt" -le "$CLIFF_FAILURES" ]; then
  printf '%s\n' 'partial context that must never be used'
  printf '%s\n' "$CLIFF_ERROR" >&2
  exit 101
fi
printf '%s\n' "$CLIFF_RESULT"
`,
						"sleep": "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> sleep-calls\n",
					} {
						if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil { // #nosec G306 -- test fixture
							t.Fatal(err)
						}
					}
					// A prior or failed attempt must not be appended to a good one.
					if err := os.WriteFile(filepath.Join(dir, "cliff-context.json"), []byte("stale context"), 0o600); err != nil {
						t.Fatal(err)
					}
					out, log, err := runStep(t, workflow.script, dir,
						"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
						"LINE_BUMP="+workflow.bump,
						"CLIFF_FAILURES="+strconv.Itoa(tc.failures),
						"CLIFF_ERROR="+tc.cause, "CLIFF_RESULT="+tc.context,
					)
					if tc.failures == 3 {
						var exitErr *exec.ExitError
						if !errors.As(err, &exitErr) || exitErr.ExitCode() != 101 {
							t.Fatalf("error = %v, want git-cliff exit 101\n%s", err, log)
						}
						if len(out) != 0 {
							t.Errorf("failed step published outputs: %v", out)
						}
						if !strings.Contains(log, "::error title=git-cliff failed::git-cliff failed after 3 attempts (exit 101)") {
							t.Errorf("missing final failure diagnostic:\n%s", log)
						}
					} else {
						if err != nil {
							t.Fatalf("compute step failed: %v\n%s", err, log)
						}
						wantVersion := "v1.2.3"
						if workflow.maintenance && tc.emptyCommits {
							wantVersion = ""
						}
						if got, ok := out["version"]; !ok || got != wantVersion {
							t.Errorf("version = %q (present %t), want %q", got, ok, wantVersion)
						}
						got, err := os.ReadFile(filepath.Join(dir, "cliff-context.json")) // #nosec G304 -- test temp directory
						if err != nil || string(got) != tc.context+"\n" {
							t.Errorf("context = %q, error = %v, want only successful context %q", got, err, tc.context)
						}
					}
					attempts := min(tc.failures+1, 3)
					calls, err := os.ReadFile(filepath.Join(dir, "cliff-calls")) // #nosec G304 -- test temp directory
					if err != nil || string(calls) != strings.Repeat(workflow.wantArgs+"\n", attempts) {
						t.Errorf("calls = %q, error = %v, want %d calls with %q", calls, err, attempts, workflow.wantArgs)
					}
					delays, err := os.ReadFile(filepath.Join(dir, "sleep-calls")) // #nosec G304 -- test temp directory
					wantDelays := []string{"", "2\n", "2\n4\n"}[attempts-1]
					if err != nil && (attempts != 1 || !os.IsNotExist(err)) {
						t.Fatal(err)
					}
					if string(delays) != wantDelays {
						t.Errorf("backoff = %q, want %q", delays, wantDelays)
					}
					if tc.failures > 0 && strings.Count(log, tc.cause) != tc.failures {
						t.Errorf("metadata cause missing from attempt logs:\n%s", log)
					}
				})
			}
		})
	}
}
