package rerun

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// now is the clock of every test; the pipelines are created relative to it.
var now = time.Date(2026, 10, 9, 18, 30, 0, 0, time.UTC)

const (
	hooksPath      = "GET /repos/o/r/hooks"
	deliveriesPath = "GET /repos/o/r/hooks/1/deliveries"
	deliveryPath   = "GET /repos/o/r/hooks/1/deliveries/42"
	attemptsPath   = "POST /repos/o/r/hooks/1/deliveries/42/attempts"
)

// The recorded shapes of GitHub's webhook API: the repository's hooks, the
// hook's deliveries (newest first, no payload) and one delivery with its
// payload, as api.github.com answers them.
func circleCIHooks() []sequence.Response {
	return body([]any{map[string]any{
		"type": "Repository", "id": 1, "name": "web", "active": true, "events": []any{"push"},
		"config": map[string]any{"content_type": "json", "insecure_ssl": "0", "url": "https://circleci.com/hooks/github"},
		"url":    "https://api.github.com/repos/o/r/hooks/1", "deliveries_url": "https://api.github.com/repos/o/r/hooks/1/deliveries",
	}})
}

func delivery(id int, guid, event string, redelivery bool, at string) map[string]any {
	return map[string]any{
		"id": id, "guid": guid, "delivered_at": at, "redelivery": redelivery, "duration": 0.27,
		"status": "OK", "status_code": 200, "event": event, "action": nil, "installation_id": nil, "repository_id": 456,
	}
}

func pushDelivery(ref, after string) []sequence.Response {
	return body(map[string]any{
		"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "redelivery": false, "status": "OK", "status_code": 200, "event": "push",
		"request": map[string]any{
			"headers": map[string]any{"X-GitHub-Event": "push", "X-GitHub-Delivery": "g-push"},
			"payload": map[string]any{"ref": ref, "before": "000", "after": after, "deleted": false, "repository": map[string]any{"full_name": "o/r"}},
		},
		"response": map[string]any{"headers": map[string]any{}, "payload": ""},
	})
}

var pushListed = body([]any{
	delivery(44, "g-pr", "pull_request", false, "2026-10-09T17:59:30Z"),
	delivery(42, "g-push", "push", false, "2026-10-09T17:59:00Z"),
})

var accepted = []sequence.Response{{Status: http.StatusAccepted}}

// stalledPipeline is a pipeline CircleCI created age ago and never gave a
// workflow.
func stalledPipeline(state string, age time.Duration) *circleciclient.Pipeline {
	return &circleciclient.Pipeline{ID: "p1", Number: 12, State: state, CreatedAt: now.Add(-age), VCS: circleciclient.PipelineVCS{Revision: "abc", Tag: "v1.2.3"}}
}

type redeliverRun struct {
	result   *Result
	warnings []string
	github   *githubmock.Server
	circleci *circlemock.Server
	err      error
}

// redeliverWith runs FromFailed on pipeline against the mocks, the GitHub
// hooks handed over as identity.
func redeliverWith(t *testing.T, gh, cc sequence.Routes, pipeline *circleciclient.Pipeline, ref, identity string) redeliverRun {
	t.Helper()
	ghServer, err := githubmock.Start(gh)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ghServer.Close)
	ccServer, err := circlemock.Start(cc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ccServer.Close)
	circleci, err := circleciclient.New(circleciclient.Config{Token: "cci_test", BaseURL: ccServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	github, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "gh_test", BaseURL: ghServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	run := redeliverRun{result: NewResult("o/r"), github: ghServer, circleci: ccServer}
	redelivery := Redelivery{
		Ref:   ref,
		Now:   func() time.Time { return now },
		Hooks: func(context.Context) (Hooks, string, error) { return github, identity, nil },
	}
	run.err = FromFailed(context.Background(), circleci, "o", "r", pipeline, run.result, func(w string) { run.warnings = append(run.warnings, w) }, redelivery)
	return run
}

func githubPosts(server *githubmock.Server) []string {
	var out []string
	for _, r := range server.Requests() {
		if r.Method == http.MethodPost {
			out = append(out, r.Path)
		}
	}
	return out
}

var noWorkflows = sequence.Routes{workflowsPath: body(map[string]any{"items": []any{}})}

// TestStalledPipelineRedeliversThePush: a pending pipeline without a workflow,
// older than StalledAfter, gets the tag's push delivery sent again: the one
// push among the deliveries, matched by its ref.
func TestStalledPipelineRedeliversThePush(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{
		hooksPath: circleCIHooks(), deliveriesPath: pushListed, deliveryPath: pushDelivery("refs/tags/v1.2.3", "abc"), attemptsPath: accepted,
	}, noWorkflows, stalledPipeline("pending", 30*time.Minute), "refs/tags/v1.2.3", authexec.IdentityApp)
	if run.err != nil {
		t.Fatalf("err: %v", run.err)
	}
	if got := githubPosts(run.github); len(got) != 1 || got[0] != "/repos/o/r/hooks/1/deliveries/42/attempts" {
		t.Errorf("posts: %q", got)
	}
	r := run.result.Redelivery
	if r == nil || r.Outcome != OutcomeRedelivered || r.HookID != 1 || r.DeliveryID != 42 || r.GUID != "g-push" ||
		r.HookURL != "https://circleci.com/hooks/github" || r.Ref != "refs/tags/v1.2.3" || r.After != "abc" || r.RedeliveredAt != nil {
		t.Errorf("redelivery: %+v", r)
	}
	if len(run.warnings) != 1 || !strings.Contains(run.warnings[0], "pipeline 12 has no workflow 30m0s after its creation (state pending)") ||
		!strings.Contains(run.warnings[0], "g-push") || !strings.Contains(run.warnings[0], "sent again") {
		t.Errorf("warnings: %q", run.warnings)
	}
	if run.result.Pipeline == nil || run.result.Pipeline.Number != 12 || run.result.HeadSHA != "abc" || len(run.result.Workflows) != 0 {
		t.Errorf("result: %+v", run.result)
	}
}

// TestFinishedSetupWithoutContinuationRedelivers: the setup workflow done
// and the continuation never created is the same stall; the setup workflow
// is listed with nothing to rerun.
func TestFinishedSetupWithoutContinuationRedelivers(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{
		hooksPath: circleCIHooks(), deliveriesPath: pushListed, deliveryPath: pushDelivery("refs/tags/v1.2.3", "abc"), attemptsPath: accepted,
	}, sequence.Routes{workflowsPath: body(map[string]any{"items": []any{workflow("w1", "setup", "success", "2026-10-09T18:00:00Z")}})},
		stalledPipeline("pending", time.Hour), "refs/tags/v1.2.3", authexec.IdentityApp)
	if run.err != nil {
		t.Fatalf("err: %v", run.err)
	}
	if len(githubPosts(run.github)) != 1 || run.result.Redelivery == nil || run.result.Redelivery.Outcome != OutcomeRedelivered {
		t.Errorf("posts %q, redelivery %+v", githubPosts(run.github), run.result.Redelivery)
	}
	if len(run.result.Workflows) != 1 || run.result.Workflows[0].Name != "setup" || run.result.Workflows[0].Outcome != OutcomeNothing {
		t.Errorf("workflows: %+v", run.result.Workflows)
	}
}

// TestBranchHeadMatchesTheRevision: without a ref, the push of the head
// revision is the delivery; a newer push of another revision is passed over.
func TestBranchHeadMatchesTheRevision(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{
		hooksPath: circleCIHooks(),
		deliveriesPath: body([]any{
			delivery(43, "g-newer", "push", false, "2026-10-09T18:10:00Z"),
			delivery(42, "g-push", "push", false, "2026-10-09T17:59:00Z"),
		}),
		"GET /repos/o/r/hooks/1/deliveries/43": pushDelivery("refs/heads/feature", "def"),
		deliveryPath:                           pushDelivery("refs/heads/feature", "abc"),
		attemptsPath:                           accepted,
	}, noWorkflows, stalledPipeline("pending", 10*time.Minute), "", authexec.IdentityApp)
	if run.err != nil {
		t.Fatalf("err: %v", run.err)
	}
	if got := githubPosts(run.github); len(got) != 1 || got[0] != "/repos/o/r/hooks/1/deliveries/42/attempts" {
		t.Errorf("posts: %q", got)
	}
}

// TestYoungStalledPipelineIsRefused: a pipeline younger than StalledAfter may
// still get its workflow: exit 5 naming the age, GitHub not read.
func TestYoungStalledPipelineIsRefused(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{}, noWorkflows, stalledPipeline("setup-pending", 90*time.Second), "refs/tags/v1.2.3", authexec.IdentityApp)
	if code, _ := agentcli.Outcome(run.err); code != agentcli.ExitRefused || !strings.Contains(run.err.Error(), "1m30s after its creation (state setup-pending)") ||
		!strings.Contains(run.err.Error(), "5m0s old") {
		t.Fatalf("err: %v", run.err)
	}
	if len(run.github.Requests()) != 0 || run.result.Redelivery != nil {
		t.Errorf("GitHub was read: %v", run.github.Requests())
	}
}

// TestAlreadyRedeliveredIsRefused: a push sent again before, whose new
// pipeline has no workflow either, is not sent a third time: exit 5 naming
// the earlier redelivery.
func TestAlreadyRedeliveredIsRefused(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{
		hooksPath: circleCIHooks(),
		deliveriesPath: body([]any{
			delivery(45, "g-push", "push", true, "2026-10-09T18:05:00Z"),
			delivery(42, "g-push", "push", false, "2026-10-09T17:59:00Z"),
		}),
		deliveryPath: pushDelivery("refs/tags/v1.2.3", "abc"),
	}, noWorkflows, stalledPipeline("pending", 20*time.Minute), "refs/tags/v1.2.3", authexec.IdentityApp)
	if code, _ := agentcli.Outcome(run.err); code != agentcli.ExitRefused || !strings.Contains(run.err.Error(), "already sent again at 2026-10-09T18:05:00Z") {
		t.Fatalf("err: %v", run.err)
	}
	r := run.result.Redelivery
	if len(githubPosts(run.github)) != 0 || r == nil || r.Outcome != OutcomeAlreadyRedelivered || r.RedeliveredAt == nil || !r.RedeliveredAt.Equal(time.Date(2026, 10, 9, 18, 5, 0, 0, time.UTC)) {
		t.Errorf("posts %q, redelivery %+v", githubPosts(run.github), r)
	}
}

// TestForbiddenNamesTheAppPermission: GitHub's 403 to the App is exit 8
// naming the repository permission the App lacks; to the person's gh login,
// their own access.
func TestForbiddenNamesTheAppPermission(t *testing.T) {
	forbidden := sequence.Routes{hooksPath: {{Status: http.StatusForbidden, Body: map[string]any{"message": "Resource not accessible by integration"}}}}
	run := redeliverWith(t, forbidden, noWorkflows, stalledPipeline("pending", 20*time.Minute), "refs/tags/v1.2.3", authexec.IdentityApp)
	if code, verdict := agentcli.Outcome(run.err); code != agentcli.ExitAuthRequired || verdict != agentcli.VerdictAuthRequired ||
		!strings.Contains(run.err.Error(), "the webhook listing of o/r (403)") || !strings.Contains(run.err.Error(), "giantswarm-devctl App lacks the repository permission Webhooks: read and write") {
		t.Fatalf("app: %v", run.err)
	}
	run = redeliverWith(t, forbidden, noWorkflows, stalledPipeline("pending", 20*time.Minute), "refs/tags/v1.2.3", authexec.IdentityGH)
	if code, _ := agentcli.Outcome(run.err); code != agentcli.ExitAuthRequired || !strings.Contains(run.err.Error(), "your gh login cannot manage its webhooks") {
		t.Fatalf("gh: %v", run.err)
	}
}

// TestForbiddenRedeliveryIsRecorded: a 403 to the redelivery itself (the App
// reads webhooks but may not write them) is exit 8 with the delivery recorded
// as refused.
func TestForbiddenRedeliveryIsRecorded(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{
		hooksPath: circleCIHooks(), deliveriesPath: pushListed, deliveryPath: pushDelivery("refs/tags/v1.2.3", "abc"),
		attemptsPath: {{Status: http.StatusForbidden, Body: map[string]any{"message": "Resource not accessible by integration"}}},
	}, noWorkflows, stalledPipeline("pending", 20*time.Minute), "refs/tags/v1.2.3", authexec.IdentityApp)
	if code, _ := agentcli.Outcome(run.err); code != agentcli.ExitAuthRequired || !strings.Contains(run.err.Error(), "the redelivery of o/r (403)") {
		t.Fatalf("err: %v", run.err)
	}
	if r := run.result.Redelivery; r == nil || r.Outcome != OutcomeRefused || r.DeliveryID != 42 {
		t.Errorf("redelivery: %+v", r)
	}
}

// TestNoCircleCIWebhookIsNotApplicable: a repository whose hooks post
// elsewhere has nothing to redeliver: exit 3.
func TestNoCircleCIWebhookIsNotApplicable(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{hooksPath: body([]any{map[string]any{
		"id": 2, "active": true, "config": map[string]any{"url": "https://example.com/hook"},
	}})}, noWorkflows, stalledPipeline("pending", 20*time.Minute), "refs/tags/v1.2.3", authexec.IdentityApp)
	if code, _ := agentcli.Outcome(run.err); code != agentcli.ExitNotApplicable || !strings.Contains(run.err.Error(), "no CircleCI webhook among its 1") {
		t.Fatalf("err: %v", run.err)
	}
}

// TestNoPushDeliveryIsNotApplicable: deliveries without the head's push (a
// fork's head, pushed to the fork) are exit 3; only push deliveries are
// fetched for their payload.
func TestNoPushDeliveryIsNotApplicable(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{
		hooksPath: circleCIHooks(), deliveriesPath: pushListed, deliveryPath: pushDelivery("refs/heads/feature", "other"),
	}, noWorkflows, stalledPipeline("pending", 20*time.Minute), "", authexec.IdentityApp)
	if code, _ := agentcli.Outcome(run.err); code != agentcli.ExitNotApplicable || !strings.Contains(run.err.Error(), "none of the newest 2 deliveries of its CircleCI webhook is the push of abc") {
		t.Fatalf("err: %v", run.err)
	}
	for _, r := range run.github.Requests() {
		if strings.HasSuffix(r.Path, "/deliveries/44") {
			t.Errorf("fetched the pull_request delivery: %v", run.github.Requests())
		}
	}
}

// TestFailedSetupIsRerunNotRedelivered: a pending pipeline whose setup
// workflow failed is rerun from failed like any other; GitHub is not read.
func TestFailedSetupIsRerunNotRedelivered(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{}, sequence.Routes{
		workflowsPath:                    body(map[string]any{"items": []any{workflow("w1", "setup", "failed", "2026-10-09T18:00:00Z")}}),
		"GET /api/v2/workflow/w1/job":    jobs("failed"),
		"POST /api/v2/workflow/w1/rerun": {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w2"}}},
	}, stalledPipeline("pending", time.Hour), "refs/tags/v1.2.3", authexec.IdentityApp)
	if run.err != nil {
		t.Fatalf("err: %v", run.err)
	}
	if len(run.github.Requests()) != 0 || run.result.Redelivery != nil || len(posts(run.circleci)) != 1 {
		t.Errorf("github %v, redelivery %+v, circleci posts %q", run.github.Requests(), run.result.Redelivery, posts(run.circleci))
	}
}

// TestRunningSetupIsWaitedFor: a setup workflow still running is no stall:
// exit 5 as for any running workflow, GitHub not read.
func TestRunningSetupIsWaitedFor(t *testing.T) {
	run := redeliverWith(t, sequence.Routes{}, sequence.Routes{
		workflowsPath: body(map[string]any{"items": []any{workflow("w1", "setup", "running", "2026-10-09T18:00:00Z")}}),
	}, stalledPipeline("setup", time.Hour), "refs/tags/v1.2.3", authexec.IdentityApp)
	if code, _ := agentcli.Outcome(run.err); code != agentcli.ExitRefused || !strings.Contains(run.err.Error(), "setup (running)") {
		t.Fatalf("err: %v", run.err)
	}
	if len(run.github.Requests()) != 0 {
		t.Errorf("GitHub was read: %v", run.github.Requests())
	}
}

// TestStalledWithoutHooksIsRefused: a caller without a GitHub side reports
// the stall, exit 5, and nothing else.
func TestStalledWithoutHooksIsRefused(t *testing.T) {
	server, err := circlemock.Start(noWorkflows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	client, err := circleciclient.New(circleciclient.Config{Token: "cci_test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = FromFailed(context.Background(), client, "o", "r", stalledPipeline("pending", time.Hour), NewResult("o/r"), func(string) {}, Redelivery{Now: func() time.Time { return now }})
	if code, _ := agentcli.Outcome(err); code != agentcli.ExitRefused || !strings.Contains(err.Error(), "redeliver its push webhook") {
		t.Fatalf("err: %v", err)
	}
}
