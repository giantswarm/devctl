package prwait

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

const (
	commitPath    = "GET /repos/o/r/git/commits/abc123"
	workflowsPath = "GET /repos/o/r/contents/.github/workflows"
	ciPath        = "GET /repos/o/r/contents/.github/workflows/ci.yaml"
	wfListPath    = "GET /repos/o/r/actions/workflows"
)

// emptyHead is a head with nothing reported, a pull request last updated at
// updatedAt, a head commit with message, and .github/workflows/ci.yaml with
// the trigger on (nil: no workflows directory).
func emptyHead(updatedAt time.Time, message, on string) sequence.Routes {
	gh := sequence.Routes{
		pullPath:   {pull("clean", map[string]any{"updated_at": updatedAt.UTC().Format(time.RFC3339Nano)})},
		runsPath:   {checkRuns()},
		statPath:   {noStatuses},
		actsPath:   {noRuns},
		commitPath: {body(map[string]any{"sha": sha, "message": message})},
	}
	if on == "" {
		return gh
	}
	gh[workflowsPath] = []sequence.Response{body([]any{
		map[string]any{"type": "file", "name": "ci.yaml", "path": ".github/workflows/ci.yaml"},
		map[string]any{"type": "file", "name": "README.md", "path": ".github/workflows/README.md"},
	})}
	content := "name: CI\non:\n" + on + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n    - run: true\n"
	gh[ciPath] = []sequence.Response{body(map[string]any{
		"type": "file", "path": ".github/workflows/ci.yaml", "encoding": "base64",
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	})}
	gh[wfListPath] = []sequence.Response{body(map[string]any{"total_count": 1, "workflows": []any{
		map[string]any{"id": 1, "path": ".github/workflows/ci.yaml", "state": "active"},
	}})}
	return gh
}

var longAgo = time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

func Test_Wait_noChecksPastTheGrace(t *testing.T) {
	// Minutes after the last push the head carries nothing, and the
	// repository's CI runs on pull_request: the event never arrived. Exit 4
	// no_checks at the first poll, naming the workflow and the retrigger.
	h := start(t, emptyHead(longAgo, "fix", "  pull_request:"), sequence.Routes{}, false, 2*time.Minute)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRequiredMissing || verdict(err) != agentcli.VerdictNoChecks {
		t.Fatalf("want exit 4 no_checks, got %d %v", exitCode(err), err)
	}
	for _, want := range []string{".github/workflows/ci.yaml run on pull_request", "close and reopen the pull request"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("reason: want %q in %q", want, err.Error())
		}
	}
	if want := "actions runs of .github/workflows/ci.yaml (none registered)"; len(result.Unfinished) != 1 || result.Unfinished[0] != want {
		t.Errorf("unfinished: want [%q], got %q", want, result.Unfinished)
	}
	if polls := strings.Count(h.progress.String(), "poll "); polls != 1 {
		t.Errorf("want one poll, got %d:\n%s", polls, h.progress.String())
	}
}

func Test_Wait_noChecksYoungHeadWaitsForTheRuns(t *testing.T) {
	// Right after a push the head carries nothing yet: the wait goes on, and
	// is green once the run registered and passed. The workflows are read
	// once for the head.
	gh := emptyHead(time.Now(), "fix", "  pull_request:")
	gh[runsPath] = []sequence.Response{checkRuns(), checkRuns(run("test", "completed", "success"))}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	if _, err := h.waiter.Wait(context.Background(), "o", "r", 42); err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	if !strings.Contains(h.progress.String(), "poll 1: waiting for actions runs of .github/workflows/ci.yaml (none registered)") {
		t.Errorf("progress does not name the expected runs:\n%s", h.progress.String())
	}
	reads := 0
	for _, r := range h.github.Requests() {
		if r.Path == "/repos/o/r/contents/.github/workflows/ci.yaml" {
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("want the workflow read once, got %d", reads)
	}
}

func Test_Wait_noChecksYoungHeadThatStaysEmpty(t *testing.T) {
	// The head stays empty: the wait ends no_checks once the grace has
	// passed (120 ms at the test's scale), well before its timeout.
	h := start(t, emptyHead(time.Now(), "fix", "  pull_request:"), sequence.Routes{}, false, 10*time.Minute)
	_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRequiredMissing || verdict(err) != agentcli.VerdictNoChecks {
		t.Fatalf("want exit 4 no_checks, got %d %v", exitCode(err), err)
	}
	if polls := strings.Count(h.progress.String(), "poll "); polls < 2 {
		t.Errorf("want the wait to poll through the grace, got %d poll(s):\n%s", polls, h.progress.String())
	}
}

func Test_Wait_emptyHeadStaysGreen(t *testing.T) {
	// Nothing is going to report: the head is green as before.
	for name, gh := range map[string]sequence.Routes{
		"no workflows":           emptyHead(longAgo, "fix", ""),
		"push only":              emptyHead(longAgo, "fix", "  push:\n    branches: [main]"),
		"paths filter":           emptyHead(longAgo, "fix", "  pull_request:\n    paths: ['helm/**']"),
		"skip ci":                emptyHead(longAgo, "chore: bump [skip ci]", "  pull_request:"),
		"pull_request_target":    emptyHead(longAgo, "fix", "  pull_request_target:"),
		"branches of other base": emptyHead(longAgo, "fix", "  pull_request:\n    branches: [release-*]"),
	} {
		t.Run(name, func(t *testing.T) {
			h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
			if _, err := h.waiter.Wait(context.Background(), "o", "r", 42); err != nil {
				t.Fatalf("want green, got %d %v", exitCode(err), err)
			}
		})
	}
	t.Run("inactive workflow", func(t *testing.T) {
		gh := emptyHead(longAgo, "fix", "  pull_request:")
		gh[wfListPath] = []sequence.Response{body(map[string]any{"total_count": 1, "workflows": []any{
			map[string]any{"id": 1, "path": ".github/workflows/ci.yaml", "state": "disabled_manually"},
		}})}
		h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
		if _, err := h.waiter.Wait(context.Background(), "o", "r", 42); err != nil {
			t.Fatalf("want green, got %d %v", exitCode(err), err)
		}
	})
}

func Test_runsOnPullRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		on   string
		want bool
	}{
		{"scalar", "on: pull_request", true},
		{"list", "on: [push, pull_request]", true},
		{"block list", "on:\n  - pull_request\n  - merge_group", true},
		{"null mapping", "on:\n  pull_request:\n  push:", true},
		{"empty mapping", "on:\n  pull_request: {}", true},
		{"default types", "on:\n  pull_request:\n    types: [opened, synchronize, reopened]", true},
		{"types without synchronize", "on:\n  pull_request:\n    types: [opened, labeled]", false},
		{"branches literal", "on:\n  pull_request:\n    branches: [main, master]", true},
		{"branches glob", "on:\n  pull_request:\n    branches: ['**']", true},
		{"branches other", "on:\n  pull_request:\n    branches: [release-*]", false},
		{"branches unknown syntax", "on:\n  pull_request:\n    branches: ['ma?n']", false},
		{"branches-ignore other", "on:\n  pull_request:\n    branches-ignore: [release-*]", true},
		{"branches-ignore base", "on:\n  pull_request:\n    branches-ignore: [main]", false},
		{"paths", "on:\n  pull_request:\n    paths: [go.mod]", false},
		{"paths-ignore", "on:\n  pull_request:\n    paths-ignore: ['docs/**']", false},
		{"target only", "on: pull_request_target", false},
		{"push only", "on: push", false},
		{"no on", "name: x", false},
		{"invalid", "on: [", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runsOnPullRequest([]byte(tc.on), "main"); got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func Test_skipsCI(t *testing.T) {
	for message, want := range map[string]bool{
		"fix: a bug":                        false,
		"chore: bump [skip ci]":             true,
		"Chore [CI SKIP]":                   true,
		"docs [skip actions]":               true,
		"subject\n\nskip-checks: true":      true,
		"subject\n\nskip-checks: false":     false,
		"mentions skip ci without brackets": false,
	} {
		if got := skipsCI(message); got != want {
			t.Errorf("%q: want %v, got %v", message, want, got)
		}
	}
}
