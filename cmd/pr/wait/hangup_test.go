//go:build !windows

package wait

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// envHangupGitHub carries the mock's URL to the child process of
// Test_Run_survivesHangupAndClosedStdin.
const envHangupGitHub = "DEVCTL_TEST_HANGUP_GITHUB_URL"

// Test_Run_survivesHangupAndClosedStdin runs the wait in a child process
// whose stdin is closed, sends it SIGHUP while the head is pending and wants
// the verdict once the head turns green: exit 0 and the green document.
func Test_Run_survivesHangupAndClosedStdin(t *testing.T) {
	pending := sequence.Response{Body: map[string]any{"total_count": 1, "check_runs": []any{
		map[string]any{"id": 1, "name": "go-build", "status": "in_progress", "html_url": "https://github.com/o/r/runs/1"},
	}}}
	green := sequence.Response{Body: map[string]any{"total_count": 1, "check_runs": []any{
		map[string]any{"id": 1, "name": "go-build", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/1"},
	}}}
	runs := []sequence.Response{}
	for range 6 {
		runs = append(runs, pending)
	}
	server, err := githubmock.Start(sequence.Routes{
		"GET /repos/o/r/pulls/42": {{Body: map[string]any{
			"number": 42, "state": "open", "draft": false, "mergeable_state": "clean",
			"head": map[string]any{"sha": "abc123", "ref": "feature"}, "base": map[string]any{"ref": "main"},
		}}},
		"GET /repos/o/r/commits/abc123/check-runs":    append(runs, green),
		"GET /repos/o/r/commits/abc123/status":        {{Body: map[string]any{"state": "success", "total_count": 0, "statuses": []any{}}}},
		"GET /repos/o/r/actions/runs?head_sha=abc123": {{Body: map[string]any{"total_count": 0, "workflow_runs": []any{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^Test_hangupChild$") // #nosec G204 G702 -- the test binary itself
	child.Env = append(os.Environ(), envHangupGitHub+"="+server.URL)
	stdin, closed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	child.Stdin = stdin
	stdout := &bytes.Buffer{}
	child.Stdout = stdout
	stderr, err := child.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()

	var progress strings.Builder
	waiting := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stderr)
		signalled := false
		for scanner.Scan() {
			progress.WriteString(scanner.Text() + "\n")
			if !signalled && strings.HasPrefix(scanner.Text(), "poll 1: waiting for ") {
				signalled = true
				close(waiting)
			}
		}
	}()
	select {
	case <-waiting:
	case <-done:
		_ = child.Wait()
		t.Fatalf("the child ended before its first pending poll:\n%s", progress.String())
	}
	if err := child.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("sending SIGHUP: %v", err)
	}
	<-done
	err = child.Wait()
	if err != nil {
		t.Fatalf("want the child to end with the verdict, got %v\nstderr:\n%s\nstdout:\n%s", err, progress.String(), stdout.String())
	}
	var doc map[string]any
	if err := json.NewDecoder(stdout).Decode(&doc); err != nil {
		t.Fatalf("stdout carries no document: %v", err)
	}
	if doc["exitCode"] != 0.0 || doc["verdict"] != "green" {
		t.Errorf("want exit 0 green, got exit %v %v: %v", doc["exitCode"], doc["verdict"], doc["reason"])
	}
	if !strings.Contains(progress.String(), "poll 7: green") {
		t.Errorf("want the wait to poll on after SIGHUP until green:\n%s", progress.String())
	}
}

// Test_hangupChild is the wait Test_Run_survivesHangupAndClosedStdin runs in
// a process of its own: the document goes to stdout, --progress to stderr.
func Test_hangupChild(t *testing.T) {
	url := os.Getenv(envHangupGitHub)
	if url == "" {
		t.Skip("the child process of Test_Run_survivesHangupAndClosedStdin")
	}
	endpoints := agentcli.DefaultEndpoints()
	endpoints.GitHubAPIURL = url
	r := &runner{
		gate:          func(bool) error { return nil },
		flag:          &flag{Timeout: 10 * time.Minute, Progress: true},
		stdout:        os.Stdout,
		stderr:        os.Stderr,
		requireGitHub: loggedIn,
		personGitHub:  loggedIn,
		requireCircleCI: func(context.Context) (authstore.Token, error) {
			return authstore.Token{}, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "no token in the keychain"}
		},
		endpoints: func() agentcli.Endpoints { return endpoints },
		clock:     func() (agentcli.Clock, error) { return agentcli.NewClock(0.01, nil), nil },
	}
	if err := r.Run(&cobra.Command{}, []string{"o/r", "42"}); err != nil {
		t.Fatalf("exit %d: %v", agentcli.Exit(err), err)
	}
}
