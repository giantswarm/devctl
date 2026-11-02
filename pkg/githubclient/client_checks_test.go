package githubclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
)

func Test_CheckRollup_pagesAndMapsToREST(t *testing.T) {
	pages := []string{
		`{"data":{"repository":{"object":{"statusCheckRollup":{"contexts":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[
			{"__typename":"CheckRun","databaseId":7,"name":"go-test","status":"COMPLETED","conclusion":"FAILURE","url":"https://github.com/o/r/runs/7","startedAt":"2026-10-07T17:00:00Z","completedAt":"2026-10-07T17:01:00Z","checkSuite":{"databaseId":70,"app":{"slug":"github-actions"}}}]}}}}}}`,
		`{"data":{"repository":{"object":{"statusCheckRollup":{"contexts":{"pageInfo":{"hasNextPage":false,"endCursor":"c2"},"nodes":[
			{"__typename":"CheckRun","databaseId":8,"name":"lint","status":"IN_PROGRESS","conclusion":null,"url":"https://github.com/o/r/runs/8","startedAt":null,"checkSuite":null},
			{"__typename":"StatusContext","context":"ci/circleci: build","state":"PENDING","targetUrl":"https://ci/1","createdAt":"2026-10-07T17:01:00Z"},
			{"__typename":"StatusContext","context":"never-reported","state":"EXPECTED","targetUrl":null,"createdAt":"2026-10-07T17:01:00Z"}]}}}}}}`,
	}
	var afters []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query struct {
			Variables map[string]any `json:"variables"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &query)
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" || query.Variables["oid"] != "abc123" {
			t.Errorf("want POST /graphql for abc123, got %s %s %v", r.Method, r.URL.Path, query.Variables)
		}
		afters = append(afters, query.Variables["after"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pages[len(afters)-1])
	}))
	t.Cleanup(server.Close)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	runs, statuses, err := c.CheckRollup(context.Background(), "o", "r", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]any{nil, "c1"}, afters); diff != "" {
		t.Errorf("cursors (-want +got):\n%s", diff)
	}
	if len(runs) != 2 || len(statuses) != 1 {
		t.Fatalf("want 2 check runs and 1 status, got %d and %d", len(runs), len(statuses))
	}
	first := runs[0]
	if first.GetID() != 7 || first.GetStatus() != "completed" || first.GetConclusion() != "failure" || first.GetHTMLURL() != "https://github.com/o/r/runs/7" ||
		first.GetCheckSuite().GetID() != 70 || first.GetApp().GetSlug() != "github-actions" || first.GetStartedAt().IsZero() || first.GetCompletedAt().IsZero() {
		t.Errorf("first check run mapped wrong: %+v", first)
	}
	if runs[1].GetStatus() != "in_progress" || runs[1].GetConclusion() != "" {
		t.Errorf("second check run mapped wrong: %+v", runs[1])
	}
	if s := statuses[0]; s.GetContext() != "ci/circleci: build" || s.GetState() != "pending" || s.GetTargetURL() != "https://ci/1" {
		t.Errorf("status mapped wrong: %+v", s)
	}
}

func Test_CheckRollup_unknownCommitIsNotFound(t *testing.T) {
	c, _ := newWireClient(t, http.StatusOK, `{"data":{"repository":{"object":null}}}`)
	if _, _, err := c.CheckRollup(context.Background(), "o", "r", "abc123"); !IsNotFound(err) {
		t.Errorf("want notFound, got %v", err)
	}
}

func Test_RequiredStatusContexts_protectionAndRulesets(t *testing.T) {
	const (
		notProtected = `{"message":"Branch not protected","documentation_url":"https://docs.github.com/rest/branches/branch-protection#get-branch-protection"}`
		protected    = `{"required_status_checks":{"strict":true,"contexts":["build"],"checks":[{"context":"build"},{"context":"lint"}]}}`
		noRules      = `[]`
		ruleset      = `[{"type":"required_status_checks","ruleset_source_type":"Repository","ruleset_source":"example-org/example-repo","ruleset_id":1,
			"parameters":{"strict_required_status_checks_policy":false,"required_status_checks":[{"context":"lint"},{"context":"test","integration_id":15368}]}}]`
		rulesetWithoutChecks = `[{"type":"deletion","ruleset_source_type":"Repository","ruleset_source":"example-org/example-repo","ruleset_id":2},
			{"type":"pull_request","ruleset_source_type":"Repository","ruleset_source":"example-org/example-repo","ruleset_id":2,
			"parameters":{"required_approving_review_count":1,"dismiss_stale_reviews_on_push":false,"require_code_owner_review":false,"require_last_push_approval":false,"required_review_thread_resolution":false,"allowed_merge_methods":["squash"]}}]`
	)
	type answer struct {
		status int
		body   string
	}
	testCases := []struct {
		name       string
		protection answer
		rules      answer
		want       []string
	}{
		{name: "classic protection only", protection: answer{http.StatusOK, protected}, rules: answer{http.StatusOK, noRules}, want: []string{"build", "lint"}},
		{name: "ruleset only", protection: answer{http.StatusNotFound, notProtected}, rules: answer{http.StatusOK, ruleset}, want: []string{"lint", "test"}},
		{name: "classic protection and ruleset", protection: answer{http.StatusOK, protected}, rules: answer{http.StatusOK, ruleset}, want: []string{"build", "lint", "test"}},
		{name: "neither", protection: answer{http.StatusNotFound, notProtected}, rules: answer{http.StatusOK, noRules}, want: []string{}},
		{name: "ruleset without required checks", protection: answer{http.StatusNotFound, notProtected}, rules: answer{http.StatusOK, rulesetWithoutChecks}, want: []string{}},
		{name: "protection not readable", protection: answer{http.StatusForbidden, `{"message":"Resource not accessible by integration"}`}, rules: answer{http.StatusOK, ruleset}, want: []string{"lint", "test"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var a answer
				switch r.URL.Path {
				case "/repos/example-org/example-repo/branches/main/protection":
					a = tc.protection
				case "/repos/example-org/example-repo/rules/branches/main":
					a = tc.rules
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					a = answer{http.StatusNotFound, `{"message":"Not Found"}`}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(a.status)
				_, _ = io.WriteString(w, a.body)
			}))
			t.Cleanup(server.Close)
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}

			got, err := c.RequiredStatusContexts(context.Background(), "example-org", "example-repo", "main")
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("required contexts (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_RequiredStatusContexts_protectionErrorIsReturned(t *testing.T) {
	c, _ := newWireClient(t, http.StatusInternalServerError, `{"message":"Server Error"}`)
	if _, err := c.RequiredStatusContexts(context.Background(), "example-org", "example-repo", "main"); err == nil {
		t.Error("want the protection read's error, got none")
	}
}
