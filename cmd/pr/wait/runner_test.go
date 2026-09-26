package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func newRunner(t *testing.T, routes sequence.Routes, github func(context.Context) (authstore.Token, error)) (*runner, *bytes.Buffer, *bytes.Buffer, *githubmock.Server) {
	t.Helper()
	server, err := githubmock.Start(routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	endpoints := agentcli.DefaultEndpoints()
	endpoints.GitHubAPIURL = server.URL
	r := &runner{
		gate:          func(bool) error { return nil },
		flag:          &flag{Timeout: 2 * time.Minute, Progress: true},
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

func Test_run_green(t *testing.T) {
	routes := sequence.Routes{
		"GET /repos/o/r/pulls/42": {{Body: map[string]any{
			"number": 42, "state": "open", "draft": false, "mergeable_state": "clean",
			"head": map[string]any{"sha": "abc123", "ref": "feature"}, "base": map[string]any{"ref": "main"},
		}}},
		"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
			map[string]any{"id": 1, "name": "go-build", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/1"},
		}}}},
		"GET /repos/o/r/commits/abc123/status":        {{Body: map[string]any{"state": "success", "total_count": 0, "statuses": []any{}}}},
		"GET /repos/o/r/actions/runs?head_sha=abc123": {{Body: map[string]any{"total_count": 0, "workflow_runs": []any{}}}},
	}
	r, stdout, stderr, _ := newRunner(t, routes, loggedIn)

	err := r.run(context.Background(), []string{"o/r", "42"})
	if err != nil {
		t.Fatalf("want exit 0, got %v", err)
	}
	doc := decode(t, stdout)
	for key, want := range map[string]any{"command": "pr wait", "schemaVersion": 1.0, "exitCode": 0.0, "verdict": "green", "reason": "", "repository": "o/r", "number": 42.0, "headSha": "abc123", "baseRef": "main"} {
		if doc[key] != want {
			t.Errorf("%s: want %v, got %v", key, want, doc[key])
		}
	}
	for _, key := range []string{"warnings", "startedAt", "finishedAt", "checks", "actions"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("missing %s in %v", key, doc)
		}
	}
	if _, ok := doc["circleci"]; ok {
		t.Error("circleci is absent when CircleCI is not consulted")
	}
	if _, ok := doc["unfinished"]; ok {
		t.Error("unfinished is absent on green")
	}
	if !strings.Contains(stderr.String(), "poll 1: green") {
		t.Errorf("--progress writes to stderr:\n%s", stderr.String())
	}
}

func Test_run_authRequiredBeforeAnyRequest(t *testing.T) {
	r, stdout, _, server := newRunner(t, sequence.Routes{}, func(context.Context) (authstore.Token, error) {
		return authstore.Token{}, &authstore.AuthRequiredError{Identity: "GitHub", Cause: "no token in the keychain", Hint: "devctl auth login --github-only"}
	})
	err := r.run(context.Background(), []string{"o/r", "42"})
	var exitErr *agentcli.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitAuthRequired {
		t.Fatalf("want exit 8, got %v", err)
	}
	doc := decode(t, stdout)
	if doc["verdict"] != "auth_required" || !strings.Contains(doc["reason"].(string), "devctl auth login") {
		t.Errorf("document: %v", doc)
	}
	if doc["repository"] != "o/r" || doc["number"] != 42.0 {
		t.Errorf("the document names the pull request even before the gate: %v", doc)
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
	r, stdout, _, _ := newRunner(t, routes, loggedInFromKeychain)

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
		t.Errorf("pr wait reads no environment override, so the hint should not suggest setting one: %v", doc)
	}
}

// The same 404 with a token from the environment (not the App login) gets no
// hint: [authstore.GitHubNotFoundHint] only explains the App's own reach.
func Test_run_notFoundNoHintForEnvToken(t *testing.T) {
	routes := sequence.Routes{
		"GET /repos/o/r/pulls/42": {{Status: 404, Body: map[string]any{"message": "Not Found"}}},
	}
	r, stdout, _, _ := newRunner(t, routes, loggedIn)

	err := r.run(context.Background(), []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Fatalf("want exit 7, got %v", err)
	}
	doc := decode(t, stdout)
	reason, _ := doc["reason"].(string)
	if strings.Contains(reason, "reaches the giantswarm organization") {
		t.Errorf("an env token got the App-installation hint: %v", doc)
	}
}

func Test_run_usage(t *testing.T) {
	for _, args := range [][]string{{}, {"o/r"}, {"r", "42"}, {"o/r", "x"}, {"o/r", "0"}, {"o/r/x", "1"}} {
		r, stdout, _, server := newRunner(t, sequence.Routes{}, loggedIn)
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

// Test_run_outdated: an outdated devctl ends the wait in the document, exit
// 7 with the gate's reason, before the keychain or GitHub is read.
func Test_run_outdated(t *testing.T) {
	r, stdout, _, _ := newRunner(t, sequence.Routes{}, func(context.Context) (authstore.Token, error) {
		t.Fatal("the keychain is read past the version gate")
		return authstore.Token{}, nil
	})
	r.gate = func(bool) error { return errors.New("version 9.0.0 of devctl is released; this is 8.0.0") }

	err := r.run(context.Background(), []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Fatalf("exit %d (%v), want %d", agentcli.Exit(err), err, agentcli.ExitUsage)
	}
	doc := decode(t, stdout)
	if doc["verdict"] != string(agentcli.VerdictUsage) || !strings.Contains(doc["reason"].(string), "version 9.0.0 of devctl is released") {
		t.Errorf("envelope: %v", doc)
	}
}

// expiringRoutes is a pull request whose check runs are pending at the first
// poll and green at the next; GitHub answers the second poll's first read
// with 401, the App user token having expired in between.
func expiringRoutes(afterExpiry ...sequence.Response) sequence.Routes {
	pr := sequence.Response{Body: map[string]any{
		"number": 42, "state": "open", "draft": false, "mergeable_state": "clean",
		"head": map[string]any{"sha": "abc123", "ref": "feature"}, "base": map[string]any{"ref": "main"},
	}}
	expired := sequence.Response{Status: 401, Body: map[string]any{"message": "Bad credentials", "status": "401"}}
	checks := func(status, conclusion string) sequence.Response {
		return sequence.Response{Body: map[string]any{"total_count": 1, "check_runs": []any{
			map[string]any{"id": 1, "name": "go-build", "status": status, "conclusion": conclusion, "html_url": "https://github.com/o/r/runs/1"},
		}}}
	}
	return sequence.Routes{
		"GET /repos/o/r/pulls/42":                     append([]sequence.Response{pr, expired}, afterExpiry...),
		"GET /repos/o/r/commits/abc123/check-runs":    {checks("in_progress", ""), checks("completed", "success")},
		"GET /repos/o/r/commits/abc123/status":        {{Body: map[string]any{"state": "success", "total_count": 0, "statuses": []any{}}}},
		"GET /repos/o/r/actions/runs?head_sha=abc123": {{Body: map[string]any{"total_count": 0, "workflow_runs": []any{}}}},
	}
}

// The token expires between two polls: the refused read is sent again with
// the renewed token and the wait goes on from where it was, green at poll 2.
func Test_run_tokenExpiresMidWait(t *testing.T) {
	pr := sequence.Response{Body: map[string]any{
		"number": 42, "state": "open", "draft": false, "mergeable_state": "clean",
		"head": map[string]any{"sha": "abc123", "ref": "feature"}, "base": map[string]any{"ref": "main"},
	}}
	r, stdout, stderr, server := newRunner(t, expiringRoutes(pr), loggedIn)
	var rejected []string
	r.renewGitHub = func(_ context.Context, token string) (string, error) {
		rejected = append(rejected, token)
		return "ghu_renewed", nil
	}

	if err := r.run(context.Background(), []string{"o/r", "42"}); err != nil {
		t.Fatalf("want exit 0, got %v\n%s", err, stderr.String())
	}
	if doc := decode(t, stdout); doc["verdict"] != "green" {
		t.Fatalf("document: %v", doc)
	}
	if strings.Join(rejected, ",") != "ghu_test" {
		t.Fatalf("renewal asked for %v, want once for the expired token", rejected)
	}
	if !strings.Contains(stderr.String(), "poll 2: green") {
		t.Errorf("the wait keeps its place across the renewal:\n%s", stderr.String())
	}
	var sent []string
	for _, req := range server.Requests() {
		if req.Path == "/repos/o/r/pulls/42" {
			sent = append(sent, req.Header.Get("Authorization"))
		}
	}
	if want := "Bearer ghu_test,Bearer ghu_test,Bearer ghu_renewed"; strings.Join(sent, ",") != want {
		t.Fatalf("pull request reads sent %v, want %s", sent, want)
	}
}

// A renewal that fails ends the wait with exit 8, naming the login.
func Test_run_tokenExpiresMidWaitRefreshRefused(t *testing.T) {
	r, stdout, _, _ := newRunner(t, expiringRoutes(), loggedIn)
	r.renewGitHub = func(context.Context, string) (string, error) {
		return "", &authstore.AuthRequiredError{Identity: "GitHub", Cause: "the token expired and GitHub refused the refresh", Hint: "devctl auth login --github-only"}
	}

	err := r.run(context.Background(), []string{"o/r", "42"})
	var exitErr *agentcli.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitAuthRequired {
		t.Fatalf("want exit 8, got %v", err)
	}
	if doc := decode(t, stdout); doc["verdict"] != "auth_required" || !strings.Contains(doc["reason"].(string), "devctl auth login --github-only") {
		t.Fatalf("document: %v", doc)
	}
}

// GitHub refusing the renewed token too ends the wait with exit 7 and
// GitHub's answer.
func Test_run_renewedTokenRefused(t *testing.T) {
	expired := sequence.Response{Status: 401, Body: map[string]any{"message": "Bad credentials", "status": "401"}}
	r, stdout, _, _ := newRunner(t, expiringRoutes(expired), loggedIn)
	r.renewGitHub = func(context.Context, string) (string, error) { return "ghu_renewed", nil }

	err := r.run(context.Background(), []string{"o/r", "42"})
	var exitErr *agentcli.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitUsage {
		t.Fatalf("want exit 7, got %v", err)
	}
	if doc := decode(t, stdout); !strings.Contains(doc["reason"].(string), "401 Bad credentials") {
		t.Fatalf("document: %v", doc)
	}
}
