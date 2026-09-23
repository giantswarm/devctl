package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// A bearer muster refuses is the auth store's exit-8 error naming the
// muster login, whatever the tool; an endpoint that does not answer is not.
func TestCallAuthRequired(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refusing.Close()
	s := &Session{Caller: &manager.Client{Endpoint: refusing.URL, Token: "stale"}, Endpoint: refusing.URL}
	_, err := s.Call(context.Background(), manager.ToolGetInfo, nil)
	require.True(t, errors.Is(err, authstore.ErrAuthRequired), err)
	require.Contains(t, err.Error(), "devctl auth login --muster-only")
	require.Contains(t, err.Error(), refusing.URL)

	s = &Session{Caller: &manager.Client{Endpoint: "http://127.0.0.1:1/mcp", Token: "x"}, Endpoint: "http://127.0.0.1:1/mcp"}
	_, err = s.Call(context.Background(), manager.ToolGetInfo, nil)
	require.Error(t, err)
	require.False(t, errors.Is(err, authstore.ErrAuthRequired), "an unreachable endpoint is not an auth error")
}

// WriteArgs adds dryRun: true, or mode: commit, never both.
func TestWriteArgs(t *testing.T) {
	require.Equal(t, map[string]any{"repository": "x", "dryRun": true}, WriteArgs(map[string]any{"repository": "x"}, true))
	require.Equal(t, map[string]any{"mode": "commit"}, WriteArgs(nil, false))
}

// The plan and the outcome print the pull request, the ask and the notice.
func TestPrintPlanAndCommitted(t *testing.T) {
	var out bytes.Buffer
	PrintPlan(&out, &manager.Plan{
		Repository: "giantswarm/old-tool", Team: "team-planeteers", FromTeam: "team-bumblebee", Accepted: true,
		Before: "- name: old-tool\n  componentType: tool\n", Entry: "- name: old-tool\n  componentType: tool\n  align: true\n",
		PullRequest: manager.PlannedPullRequest{Title: "chore(team-planeteers): take over old-tool", Branch: "reposetup/transfer-old-tool", Files: []string{"repositories/team-bumblebee.yaml", "repositories/team-planeteers.yaml"}, As: "alice"},
		Ask:         &manager.PlannedMessage{Team: "team-planeteers", Channel: "team-planeteers", Deliverable: true},
		Notice:      &manager.PlannedMessage{Team: "team-bumblebee", Deliverable: false, Reason: "no standup channel in the policy file"},
	})
	text := out.String()
	for _, want := range []string{
		"dry run: nothing written",
		"giantswarm/old-tool in team-bumblebee -> team-planeteers: accepted",
		"entry before:\n  - name: old-tool\n    componentType: tool",
		"entry after:\n  - name: old-tool\n    componentType: tool\n    align: true",
		"pull request: chore(team-planeteers): take over old-tool (branch reposetup/transfer-old-tool, repositories/team-bumblebee.yaml, repositories/team-planeteers.yaml) as alice",
		"ask: to team-planeteers in #team-planeteers",
		"notice: to team-bumblebee, not deliverable: no standup channel in the policy file",
	} {
		require.Contains(t, text, want)
	}

	out.Reset()
	at := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	PrintCommitted(&out, &manager.Committed{
		PullRequest: &manager.PullRequest{URL: "https://github.com/giantswarm/github/pull/6179", Title: "chore: archive old-tool", AutoMerge: true},
		Ask:         &manager.Delivery{Team: "team-bumblebee", Channel: "team-bumblebee", Delivered: true},
		Notice:      &manager.Delivery{Team: "team-planeteers", Delivered: false, Error: "gateway unreachable"},
		PendingRun:  &manager.PendingRun{DispatchedAt: at, By: "alice", Kind: "archived", PullRequest: &manager.ChangePullRequest{Number: 6179, URL: "https://github.com/giantswarm/github/pull/6179"}},
	})
	text = out.String()
	for _, want := range []string{
		"pull request: https://github.com/giantswarm/github/pull/6179 (chore: archive old-tool; opened, auto-merge armed)",
		"ask: delivered to team-bumblebee in #team-bumblebee",
		"notice: not delivered to team-planeteers: gateway unreachable",
		"pending run: expected since 2026-09-22T10:00:00Z by alice (archived, https://github.com/giantswarm/github/pull/6179)",
	} {
		require.Contains(t, text, want)
	}
}

// The record renderer names every block, and says so when the declaration
// or the repository is missing.
func TestPrintRecord(t *testing.T) {
	followed, arm := true, false
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	PrintRecord(&out, &manager.Record{
		Repository: "giantswarm/my-service", Source: "sweep", Age: "5m",
		Declaration: &manager.Declaration{Team: "team-bumblebee", File: "repositories/team-bumblebee.yaml", ComponentType: "service", Language: "go", Flavours: []string{"app"}, Accepted: true, Entry: "- name: my-service\n"},
		Reality:     &manager.Reality{URL: "https://github.com/giantswarm/my-service", Visibility: "public", DefaultBranch: "main", LastPersonCommit: &manager.Commit{Date: at, Author: "alice"}, LatestRelease: &manager.Release{Tag: "v1.0.0", Build: &manager.Statuses{State: "success"}}},
		CircleCI:    &manager.CircleCIFacts{Followed: &followed, Webhook: &followed, Head: &manager.Statuses{State: "success"}, Source: "statuses+artifact"},
		CI:          &manager.CIFacts{Generated: true, Orb: "10.5.0", ARM64: &arm, ChinaPush: "split", Signing: "unsigned", SigningReason: "a private repository"},
		Renovate:    &manager.RenovateFacts{Configured: true, Enabled: true, Path: "renovate.json5", DashboardIssue: &manager.Issue{Number: 3}},
		Catalog:     &manager.Presence{Present: true}, Mapping: &manager.Presence{Present: true, Team: "bumblebee"},
		Setup:    manager.Setup{CheckError: "no read identity"},
		Findings: []manager.Finding{{Kind: "default-icon", Message: "the default icon", Fix: "replace it", Source: "engine"}},
	})
	text := out.String()
	for _, want := range []string{
		"giantswarm/my-service (record from sweep, 5m old)",
		"declaration: team-bumblebee (repositories/team-bumblebee.yaml): service, go, flavours app, lifecycle production, accepted",
		"  - name: my-service",
		"github: https://github.com/giantswarm/my-service · public · default branch main · last person commit 2026-09-01 by alice · latest release v1.0.0 (build success)",
		"circleci: followed yes, setup workflows unknown, webhook present, head success, from statuses+artifact",
		"ci: generated, orb 10.5.0, arm64 no, china push split, signing unsigned (a private repository)",
		"renovate: configured (renovate.json5), dashboard #3",
		"catalog present · mapping present (team bumblebee)",
		"set-up: not checked: no read identity",
		"findings:\n  default-icon: the default icon -- fix: replace it (engine)",
	} {
		require.Contains(t, text, want)
	}

	out.Reset()
	PrintRecord(&out, &manager.Record{Repository: "giantswarm/orphan"})
	require.Contains(t, out.String(), "declaration: none -- no team file declares this repository")
	require.Contains(t, out.String(), "github: gone")
	require.Contains(t, out.String(), "set-up: not checked\n")

	out.Reset()
	PrintRecord(&out, &manager.Record{
		Repository:  "giantswarm/legacy",
		Declaration: &manager.Declaration{Team: "team-bumblebee", File: "repositories/team-bumblebee.yaml", Problems: []string{"name: must not end in -app"}},
	})
	require.Contains(t, out.String(), "declaration: team-bumblebee (repositories/team-bumblebee.yaml): lifecycle production, refused\n  name: must not end in -app\n")
}

// A repository argument is the name with or without the org; an empty or
// half one is refused.
func TestRepository(t *testing.T) {
	for _, ok := range []string{"my-service", "giantswarm/my-service"} {
		got, err := Repository(ok)
		require.NoError(t, err)
		require.Equal(t, ok, got)
	}
	for _, bad := range []string{"", " ", "giantswarm/", "/x"} {
		_, err := Repository(bad)
		require.True(t, IsInvalidFlag(err), bad)
	}
}

// PrintJSON prints JSON indented and text as it is.
func TestPrintJSON(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, PrintJSON(&out, json.RawMessage(`{"b":1,"a":[1,2]}`)))
	require.Equal(t, "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": 1\n}\n", out.String())
	out.Reset()
	require.NoError(t, PrintJSON(&out, json.RawMessage("Server 'x' is already authenticated.")))
	require.True(t, strings.HasPrefix(out.String(), "Server 'x'"))
}
