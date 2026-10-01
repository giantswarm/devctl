package merge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func newRunner(t *testing.T, routes sequence.Routes, github func(context.Context) (authstore.Token, error), f *flag) (*runner, *bytes.Buffer, *bytes.Buffer, *githubmock.Server) {
	t.Helper()
	server, err := githubmock.Start(routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	endpoints := agentcli.DefaultEndpoints()
	endpoints.GitHubAPIURL = server.URL
	if f == nil {
		f = &flag{Timeout: 2 * time.Minute, ReleaseTimeout: 2 * time.Minute, Progress: true}
	}
	r := &runner{
		gate:          func(bool) error { return nil },
		flag:          f,
		stdout:        stdout,
		stderr:        stderr,
		requireGitHub: github,
		requireCircleCI: func(context.Context) (authstore.Token, error) {
			return authstore.Token{}, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "no token in the keychain", Hint: "devctl auth login --circleci-only"}
		},
		endpoints: func() agentcli.Endpoints { return endpoints },
		clock:     func() (agentcli.Clock, error) { return agentcli.NewClock(0.001, nil), nil },
	}
	return r, stdout, stderr, server
}

func loggedIn(context.Context) (authstore.Token, error) {
	return authstore.Token{Value: "ghu_test", Login: "someone"}, nil
}

func loggedInFromKeychain(context.Context) (authstore.Token, error) {
	return authstore.Token{Value: "ghu_test", Login: "someone", Source: authstore.SourceKeychain}, nil
}

func decode(t *testing.T, stdout *bytes.Buffer) map[string]any {
	t.Helper()
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout.String())
	}
	if dec.More() {
		t.Fatalf("stdout carries more than one document:\n%s", stdout.String())
	}
	return doc
}

// green is o/r#42 opened by the caller with one passed check, merging as m1.
func green() sequence.Routes {
	return sequence.Routes{
		"GET /repos/o/r/pulls/42": {{Body: map[string]any{
			"number": 42, "state": "open", "draft": false, "mergeable_state": "clean", "title": "feat: thing", "node_id": "PR_1",
			"user": map[string]any{"login": "someone", "type": "User"},
			"head": map[string]any{"sha": "abc123", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}},
			"base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "o/r"}},
		}}},
		"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
			map[string]any{"id": 1, "name": "go-build", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/1"},
		}}}},
		"GET /repos/o/r/commits/abc123/status":        {{Body: map[string]any{"state": "success", "total_count": 0, "statuses": []any{}}}},
		"GET /repos/o/r/actions/runs?head_sha=abc123": {{Body: map[string]any{"total_count": 0, "workflow_runs": []any{}}}},
		"PUT /repos/o/r/pulls/42/merge":               {{Body: map[string]any{"sha": "m1", "merged": true, "message": "Pull Request successfully merged"}}},
		"DELETE /repos/o/r/git/refs/heads/feature":    {{Status: http.StatusNoContent}},
	}
}

func Test_run_merged(t *testing.T) {
	r, stdout, stderr, _ := newRunner(t, green(), loggedIn, nil)

	err := r.run(context.Background(), []string{"o/r", "42"})
	if err != nil {
		t.Fatalf("want exit 0, got %v\n%s", err, stderr.String())
	}
	doc := decode(t, stdout)
	for key, want := range map[string]any{
		"command": "pr merge", "schemaVersion": 1.0, "exitCode": 0.0, "verdict": "green", "reason": "",
		"repository": "o/r", "number": 42.0, "headSha": "abc123", "baseRef": "main",
		"mergeCommitSha": "m1", "method": "squash", "branchDeleted": true, "enqueued": false,
	} {
		if doc[key] != want {
			t.Errorf("%s: want %v, got %v", key, want, doc[key])
		}
	}
	// o/r has no team-file entry and no release workflow at m1: no release
	// follows the merge, which is exit 0.
	release, _ := doc["release"].(map[string]any)
	if release["verdict"] != "no_release" || release["sha"] != "m1" || !strings.Contains(release["reason"].(string), "nothing tags the merge commit of o/r#42") {
		t.Errorf("release: %v", doc["release"])
	}
	for _, key := range []string{"warnings", "startedAt", "finishedAt", "checks", "actions"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("missing %s in %v", key, doc)
		}
	}
	if !strings.Contains(stderr.String(), "merged: squash of abc123 is m1") {
		t.Errorf("--progress writes to stderr:\n%s", stderr.String())
	}
}

func Test_run_rebaseFlag(t *testing.T) {
	r, stdout, _, _ := newRunner(t, green(), loggedIn, &flag{Timeout: time.Minute, Rebase: true, NoReleaseWait: true})
	if err := r.run(context.Background(), []string{"o/r", "42"}); err != nil {
		t.Fatal(err)
	}
	if doc := decode(t, stdout); doc["method"] != "rebase" {
		t.Errorf("--rebase: want method rebase, got %v", doc["method"])
	}
}

// --no-release-wait ends at the merge: the document's release is null and
// nothing is read after the branch is deleted.
func Test_run_noReleaseWait(t *testing.T) {
	r, stdout, _, server := newRunner(t, green(), loggedIn, &flag{Timeout: time.Minute, NoReleaseWait: true})
	if err := r.run(context.Background(), []string{"o/r", "42"}); err != nil {
		t.Fatal(err)
	}
	doc := decode(t, stdout)
	if release, ok := doc["release"]; !ok || release != nil {
		t.Errorf("want release null, got %v (present %v)", release, ok)
	}
	requests := server.Requests()
	if last := requests[len(requests)-1]; last.Method != http.MethodDelete {
		t.Errorf("want the branch deletion last, got %s", last)
	}
}

func Test_run_releaseTimeoutMustBePositive(t *testing.T) {
	r, stdout, _, server := newRunner(t, green(), loggedIn, &flag{Timeout: time.Minute})
	err := r.run(context.Background(), []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(err.Error(), "--release-timeout must be positive") {
		t.Fatalf("want exit 7 naming --release-timeout, got %v", err)
	}
	if doc := decode(t, stdout); doc["verdict"] != "usage" {
		t.Errorf("document: %v", doc)
	}
	if n := len(server.Requests()); n != 0 {
		t.Errorf("want no request, got %d", n)
	}
}

func Test_run_refusedCarriesTheMergeFields(t *testing.T) {
	routes := green()
	routes["GET /repos/o/r/pulls/42"][0].Body.(map[string]any)["user"] = map[string]any{"login": "alice", "type": "User"}
	r, stdout, _, server := newRunner(t, routes, loggedIn, nil)
	err := r.run(context.Background(), []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitRefused {
		t.Fatalf("want exit 5, got %v", err)
	}
	doc := decode(t, stdout)
	if doc["verdict"] != "refused" || doc["mergeCommitSha"] != "" || doc["branchDeleted"] != false || doc["method"] != "squash" {
		t.Errorf("document: %v", doc)
	}
	for _, req := range server.Requests() {
		if req.Method != http.MethodGet {
			t.Errorf("a refusal writes nothing: %s", req)
		}
	}
}

func Test_run_authRequiredBeforeAnyRequest(t *testing.T) {
	r, stdout, _, server := newRunner(t, sequence.Routes{}, func(context.Context) (authstore.Token, error) {
		return authstore.Token{}, &authstore.AuthRequiredError{Identity: "GitHub", Cause: "no token in the keychain", Hint: "devctl auth login --github-only"}
	}, nil)
	err := r.run(context.Background(), []string{"o/r", "42"})
	var exitErr *agentcli.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitAuthRequired {
		t.Fatalf("want exit 8, got %v", err)
	}
	doc := decode(t, stdout)
	if doc["verdict"] != "auth_required" || !strings.Contains(doc["reason"].(string), "devctl auth login") {
		t.Errorf("document: %v", doc)
	}
	if n := len(server.Requests()); n != 0 {
		t.Errorf("want no request before the gate, got %d", n)
	}
}

// A pull request the App's token cannot see (e.g. a private repository
// outside its installation) answers 404 like one that does not exist; the
// keychain token's App login gets a hint naming what it reaches, so this
// stays distinguishable from a genuinely missing pull request.
func Test_run_notFoundHint(t *testing.T) {
	routes := sequence.Routes{
		"GET /repos/o/r/pulls/42": {{Status: 404, Body: map[string]any{"message": "Not Found"}}},
	}
	r, stdout, _, _ := newRunner(t, routes, loggedInFromKeychain, nil)

	err := r.run(context.Background(), []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Fatalf("want exit 7, got %v", err)
	}
	doc := decode(t, stdout)
	reason, _ := doc["reason"].(string)
	if !strings.Contains(reason, "not found error: pull request o/r#42") {
		t.Errorf("reason lost the original not-found error: %v", doc)
	}
	if !strings.Contains(reason, "reaches the giantswarm organization and public repositories only") {
		t.Errorf("reason has no hint for the App-token 404: %v", doc)
	}
	if strings.Contains(reason, "set $") {
		t.Errorf("pr merge reads no environment override, so the hint should not suggest setting one: %v", doc)
	}
}

func Test_run_usage(t *testing.T) {
	for _, args := range [][]string{{}, {"o/r"}, {"r", "42"}, {"o/r", "x"}, {"o/r", "0"}, {"o/r/x", "1"}} {
		r, stdout, _, server := newRunner(t, sequence.Routes{}, loggedIn, nil)
		err := r.run(context.Background(), args)
		if agentcli.Exit(err) != agentcli.ExitUsage {
			t.Errorf("%q: want exit 7, got %v", args, err)
		}
		if doc := decode(t, stdout); doc["verdict"] != "usage" {
			t.Errorf("%q: want the usage verdict, got %v", args, doc["verdict"])
		}
		if n := len(server.Requests()); n != 0 {
			t.Errorf("%q: want no request, got %d", args, n)
		}
	}
}
