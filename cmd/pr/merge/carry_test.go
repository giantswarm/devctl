package merge

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func Test_run_carryOf(t *testing.T) {
	routes := green()
	pr := routes["GET /repos/o/r/pulls/42"][0].Body.(map[string]any)
	pr["base"].(map[string]any)["sha"] = "base123"
	routes["GET /repos/o/r/pulls/12"] = []sequence.Response{{Body: map[string]any{"number": 12, "user": map[string]any{"id": 7, "login": "alice"}}}}
	routes["GET /repos/o/r/compare/base123...abc123"] = []sequence.Response{{Body: map[string]any{"total_commits": 1, "commits": []any{
		map[string]any{"sha": "abc123", "author": map[string]any{"id": 7}, "commit": map[string]any{"author": map[string]any{"name": "Alice", "email": "alice@example.invalid"}}},
	}}}}
	f := &flag{}
	cmd := &cobra.Command{}
	f.Init(cmd)
	if err := cmd.ParseFlags([]string{"--carry-of=12", "--no-release-wait"}); err != nil {
		t.Fatal(err)
	}
	r, stdout, _, _ := newRunner(t, routes, loggedIn, f)
	if err := r.run(t.Context(), []string{"o/r", "42"}); err != nil {
		t.Fatal(err)
	}
	doc := decode(t, stdout)
	carry, _ := doc["carryOf"].(map[string]any)
	if doc["method"] != "rebase" || carry["pullRequest"] != "https://github.com/o/r/pull/12" || carry["author"] != "alice" || doc["mergedBy"] != "someone" {
		t.Errorf("carry document: %v", doc)
	}
	if args := f.childArgs("o/r", 42, "test-handle"); !strings.Contains(strings.Join(args, " "), "--carry-of=12") {
		t.Errorf("detached child loses carry authorship: %v", args)
	}
}

func Test_run_carryOfInvalid(t *testing.T) {
	for _, value := range []string{"0", "-1", "42", "alice", "12 --squash"} {
		t.Run(value, func(t *testing.T) {
			f := &flag{Timeout: time.Minute, NoReleaseWait: true, CarryOf: value}
			if value == "12 --squash" {
				f.CarryOf, f.Squash = "12", true
			}
			r, stdout, _, server := newRunner(t, green(), loggedIn, f)
			err := r.run(t.Context(), []string{"o/r", "42"})
			if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(err.Error(), "--carry-of") {
				t.Fatalf("want usage error naming --carry-of, got %v", err)
			}
			if doc := decode(t, stdout); doc["verdict"] != "usage" || len(server.Requests()) != 0 {
				t.Errorf("invalid carry must fail before GitHub: %v", doc)
			}
		})
	}
}

func Test_Run_carryOfEmptyFlag(t *testing.T) {
	f := &flag{}
	cmd := &cobra.Command{}
	f.Init(cmd)
	if err := cmd.ParseFlags([]string{"--carry-of="}); err != nil {
		t.Fatal(err)
	}
	r, stdout, _, server := newRunner(t, green(), loggedIn, f)
	err := r.Run(cmd, []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitUsage || len(server.Requests()) != 0 {
		t.Fatalf("an empty --carry-of must not silently squash: %v, %s", err, stdout)
	}
}
