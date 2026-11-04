package checks

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

const fixtureRepoPath = "/repos/fixture/repository"

type checksWrite struct {
	method string
	path   string
	body   []byte
}

// checksFixture replaces the entire HTTP transport. Unexpected requests fail
// closed, and no socket, keychain or real token is used. Tests using it must not
// run in parallel because the command constructs its client from the defaults.
type checksFixture struct {
	t           *testing.T
	protection  *github.Protection
	ruleset     *github.RepositoryRuleset
	reported    []string
	requests    []string
	writes      []checksWrite
	before      func(*http.Request)
	writeStatus int
}

func newChecksFixture(t *testing.T) *checksFixture {
	t.Helper()
	f := &checksFixture{t: t, protection: &github.Protection{}}
	previous := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = previous })
	// This variable is private to the fixture; never read an operator's token.
	t.Setenv("DEVCTL_CHECKS_FIXTURE_TOKEN", "synthetic-offline-token")
	return f
}

func (f *checksFixture) execute(ctx context.Context, args ...string) (string, error) {
	f.t.Helper()
	var stdout bytes.Buffer
	command, err := New(Config{Logger: logrus.New(), Stdout: &stdout, Stderr: io.Discard})
	require.NoError(f.t, err)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs(append([]string{"--github-token-envvar=DEVCTL_CHECKS_FIXTURE_TOKEN"}, args...))
	err = command.ExecuteContext(ctx)
	return stdout.String(), err
}

func (f *checksFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.before != nil {
		f.before(req)
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	f.requests = append(f.requests, req.Method+" "+req.URL.Path)
	if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || !strings.HasPrefix(req.URL.Path, fixtureRepoPath) {
		return nil, fmt.Errorf("unexpected fixture destination")
	}
	path := strings.TrimPrefix(req.URL.Path, fixtureRepoPath)
	if req.Method != http.MethodGet {
		var body []byte
		if req.Body != nil {
			var err error
			body, err = io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
		}
		f.writes = append(f.writes, checksWrite{req.Method, path, body})
		if f.writeStatus != 0 {
			return f.response(req, f.writeStatus, map[string]string{"message": "fixture write refused"})
		}
		switch {
		case req.Method == http.MethodPut && path == "/branches/main/protection":
			return f.response(req, http.StatusOK, f.protection)
		case req.Method == http.MethodPatch && path == "/branches/main/protection/required_status_checks":
			var checks github.RequiredStatusChecks
			if err := json.Unmarshal(body, &checks); err != nil {
				return nil, err
			}
			f.protection.RequiredStatusChecks = &checks
			return f.response(req, http.StatusOK, &checks)
		case req.Method == http.MethodDelete && path == "/branches/main/protection/required_status_checks":
			f.protection.RequiredStatusChecks = nil
			return f.response(req, http.StatusNoContent, nil)
		case req.Method == http.MethodPut && path == "/rulesets/7":
			var rs github.RepositoryRuleset
			if err := json.Unmarshal(body, &rs); err != nil {
				return nil, err
			}
			rs.ID = new(int64(7))
			f.ruleset = &rs
			return f.response(req, http.StatusOK, f.ruleset)
		}
		return nil, fmt.Errorf("unexpected fixture mutation: %s %s", req.Method, path)
	}
	switch path {
	case "":
		return f.response(req, http.StatusOK, map[string]any{"name": "repository", "full_name": "fixture/repository", "owner": map[string]string{"login": "fixture"}, "default_branch": "main"})
	case "/branches/main/protection":
		if f.protection != nil {
			return f.response(req, http.StatusOK, f.protection)
		}
	case "/rulesets":
		if f.ruleset != nil {
			return f.response(req, http.StatusOK, []*github.RepositoryRuleset{f.ruleset})
		}
		return f.response(req, http.StatusOK, []any{})
	case "/rulesets/7":
		return f.response(req, http.StatusOK, f.ruleset)
	case "/pulls":
		if req.URL.Query().Get("state") == "closed" && len(f.reported) > 0 {
			return f.response(req, http.StatusOK, []any{map[string]any{"merged_at": "2026-10-01T10:00:00Z", "head": map[string]string{"sha": "fixture-head"}, "merge_commit_sha": "fixture-merge"}})
		}
		return f.response(req, http.StatusOK, []any{})
	case "/commits/fixture-head/status":
		var statuses []map[string]string
		for _, name := range f.reported {
			statuses = append(statuses, map[string]string{"context": name, "state": "success"})
		}
		return f.response(req, http.StatusOK, map[string]any{"statuses": statuses})
	case "/commits/fixture-head/check-runs":
		return f.response(req, http.StatusOK, map[string]any{"check_runs": []any{}})
	case "/contents/.circleci/workflows.yml":
		return f.response(req, http.StatusOK, map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("workflows:\n  build:\n    jobs: [build]\n"))})
	case "/contents/.circleci/custom.yml", "/contents/tests/e2e":
	default:
		return nil, fmt.Errorf("unexpected fixture read: %s", path)
	}
	return f.response(req, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func (f *checksFixture) response(req *http.Request, status int, value any) (*http.Response, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
}

func fixtureRuleset(checks ...*github.RuleStatusCheck) *github.RepositoryRuleset {
	return &github.RepositoryRuleset{
		ID: new(int64(7)), Name: reconcile.RulesetName,
		Target: new(github.RulesetTargetBranch), Enforcement: github.RulesetEnforcementActive,
		BypassActors: []*github.BypassActor{{ActorID: new(int64(42)), ActorType: new(github.BypassActorTypeTeam), BypassMode: new(github.BypassModePullRequest)}},
		Conditions:   &github.RepositoryRulesetConditions{RefName: &github.RepositoryRulesetRefConditionParameters{Include: []string{"~DEFAULT_BRANCH"}, Exclude: []string{}}},
		Rules: &github.RepositoryRulesetRules{
			RequiredStatusChecks: &github.RequiredStatusChecksRuleParameters{RequiredStatusChecks: checks, StrictRequiredStatusChecksPolicy: true},
			PullRequest:          &github.PullRequestRuleParameters{RequiredApprovingReviewCount: 2, DismissStaleReviewsOnPush: true, RequireCodeOwnerReview: true},
			Deletion:             &github.EmptyRuleParameters{}, NonFastForward: &github.EmptyRuleParameters{},
		},
	}
}
