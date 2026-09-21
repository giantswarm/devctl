package prwait

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

const (
	sha      = "abc123"
	pullPath = "GET /repos/o/r/pulls/42"
	runsPath = "GET /repos/o/r/commits/abc123/check-runs"
	statPath = "GET /repos/o/r/commits/abc123/status"
	actsPath = "GET /repos/o/r/actions/runs?head_sha=abc123"
	confPath = "GET /repos/o/r/contents/.circleci/config.yml"
	projPath = "GET /api/v2/project/gh/o/r"
	pipePath = "GET /api/v2/project/gh/o/r/pipeline?branch=feature"
	wfPath   = "GET /api/v2/pipeline/p1/workflow"
)

func body(v any) sequence.Response { return sequence.Response{Body: v} }

func pull(mergeableState string, extra map[string]any) sequence.Response {
	pr := map[string]any{
		"number": 42, "state": "open", "draft": false, "merged": false,
		"mergeable_state": mergeableState,
		"head":            map[string]any{"sha": sha, "ref": "feature", "repo": map[string]any{"full_name": "o/r"}},
		"base":            map[string]any{"ref": "main", "repo": map[string]any{"full_name": "o/r"}},
	}
	for k, v := range extra {
		pr[k] = v
	}
	return body(pr)
}

func checkRuns(runs ...map[string]any) sequence.Response {
	return body(map[string]any{"total_count": len(runs), "check_runs": runs})
}

func run(name, status, conclusion string) map[string]any {
	return map[string]any{"id": 1, "name": name, "status": status, "conclusion": conclusion, "html_url": "https://github.com/o/r/runs/1", "started_at": "2026-09-21T10:00:00Z"}
}

var (
	noStatuses = body(map[string]any{"state": "pending", "total_count": 0, "statuses": []any{}})
	noRuns     = body(map[string]any{"total_count": 0, "workflow_runs": []any{}})
	config     = body(map[string]any{"type": "file", "path": ".circleci/config.yml", "content": "", "encoding": "base64"})
	project    = body(map[string]any{"slug": "gh/o/r", "name": "r"})
	pipelines  = body(map[string]any{"items": []any{map[string]any{"id": "p1", "number": 12, "state": "created", "vcs": map[string]any{"revision": sha, "branch": "feature"}}}})
)

// gitHubGreen is a head whose GitHub side is green: one successful check run,
// no statuses, no open run.
func gitHubGreen() sequence.Routes {
	return sequence.Routes{
		pullPath: {pull("clean", nil)},
		runsPath: {checkRuns(run("go-build", "completed", "success"))},
		statPath: {noStatuses},
		actsPath: {noRuns},
	}
}

type harness struct {
	github   *githubmock.Server
	circleci *circlemock.Server
	cond     *githubclient.Conditional
	waiter   *Waiter
	progress *strings.Builder
}

// start wires a Waiter to the mocks at scale 0.001: a 15 s poll interval is
// 15 ms, the two-minute timeout 120 ms. circleci nil means no CircleCI
// client is available (the token is missing).
func start(t *testing.T, gh, cc sequence.Routes, circleci bool, timeout time.Duration) *harness {
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

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	client, cond, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: ghServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	progress := &strings.Builder{}
	config := Config{
		GitHub:   client,
		Rate:     cond,
		Clock:    agentcli.NewClock(0.001, nil),
		Progress: agentcli.NewProgress(progress, true),
		Timeout:  timeout,
	}
	if circleci {
		config.CircleCI = func(context.Context) (*circleciclient.Client, error) {
			return circleciclient.New(circleciclient.Config{Token: "cci_test", BaseURL: ccServer.URL})
		}
	} else {
		config.CircleCI = func(context.Context) (*circleciclient.Client, error) {
			return nil, agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "CircleCI authentication required")
		}
	}
	waiter, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{github: ghServer, circleci: ccServer, cond: cond, waiter: waiter, progress: progress}
}

func exitCode(err error) int { return agentcli.Exit(err) }

func Test_Wait_stageGap(t *testing.T) {
	// GitHub reads green from the first poll; the CircleCI workflow behind
	// requires: is running on the first poll and success on the second.
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	cc := sequence.Routes{
		projPath: {project},
		pipePath: {pipelines},
		wfPath: {
			body(map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": "running"}}}),
			body(map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": "success"}}}),
		},
	}
	h := start(t, gh, cc, true, 2*time.Minute)

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %v", err)
	}
	want := &Result{
		Repository: "o/r", Number: 42, HeadSHA: sha, BaseRef: "main",
		Checks:  []Check{{Name: "go-build", Source: SourceCheckRun, Status: "completed", Conclusion: "success", URL: "https://github.com/o/r/runs/1"}},
		Actions: []ActionRun{},
		CircleCI: &CircleCI{PipelineID: "p1", PipelineNumber: 12, Workflows: []Workflow{
			{Name: "build", Status: "success", URL: "https://app.circleci.com/pipelines/github/o/r/12/workflows/w1"},
		}},
	}
	if diff := cmp.Diff(want, result); diff != "" {
		t.Errorf("result:\n%s", diff)
	}
	// The second poll's GitHub requests were conditional and answered 304.
	if h.cond.Replayed() < 3 {
		t.Errorf("want the second poll's pull, check-runs and status answers replayed from the ETag cache, got %d replays", h.cond.Replayed())
	}
	if !strings.Contains(h.progress.String(), "circleci workflow build (running)") {
		t.Errorf("progress names the unfinished workflow:\n%s", h.progress.String())
	}
}

func Test_Wait_gitHubAloneWithoutCircleCIConfig(t *testing.T) {
	// No .circleci/config.yml at the head: the CircleCI token is never asked
	// for and no CircleCI request is made.
	h := start(t, gitHubGreen(), sequence.Routes{}, false, 2*time.Minute)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %v", err)
	}
	if result.CircleCI != nil {
		t.Errorf("want no circleci in the document, got %+v", result.CircleCI)
	}
	if n := len(h.circleci.Requests()); n != 0 {
		t.Errorf("want no CircleCI request, got %d", n)
	}
}

func Test_Wait_circleCITokenRequired(t *testing.T) {
	// The head carries a CircleCI configuration and no token is there: exit 8
	// before any CircleCI request.
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitAuthRequired {
		t.Fatalf("want exit 8, got %d (%v)", exitCode(err), err)
	}
}

func Test_Wait_noCircleCIProjectIsGitHubAlone(t *testing.T) {
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	h := start(t, gh, sequence.Routes{}, true, 2*time.Minute)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "no CircleCI project") {
		t.Errorf("want one warning naming the missing project, got %q", result.Warnings)
	}
}

func Test_Wait_notApplicable(t *testing.T) {
	testCases := []struct {
		name string
		pull sequence.Response
		want string
	}{
		{name: "conflicting", pull: pull("dirty", nil), want: "conflicts with main"},
		{name: "behind a strict base", pull: pull("behind", nil), want: "behind main"},
		{name: "draft", pull: pull("clean", map[string]any{"draft": true}), want: "a draft"},
		{name: "closed", pull: pull("clean", map[string]any{"state": "closed"}), want: "is closed"},
		{name: "merged", pull: pull("clean", map[string]any{"state": "closed", "merged": true}), want: "is merged"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := start(t, sequence.Routes{pullPath: {tc.pull}}, sequence.Routes{}, false, 2*time.Minute)
			result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if exitCode(err) != agentcli.ExitNotApplicable || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want exit 3 naming %q, got %d %v", tc.want, exitCode(err), err)
			}
			if result.HeadSHA != sha {
				t.Errorf("the document still names the head: want %s, got %q", sha, result.HeadSHA)
			}
			if n := len(h.github.Requests()); n != 1 {
				t.Errorf("want the pull request read and nothing else, got %d requests", n)
			}
		})
	}
}

func Test_Wait_redAtOnce(t *testing.T) {
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{checkRuns(run("go-build", "completed", "failure"), run("go-test", "in_progress", ""))}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRed || !strings.Contains(err.Error(), "check go-build concluded failure") {
		t.Fatalf("want exit 1 naming go-build, got %d %v", exitCode(err), err)
	}
	if polls := strings.Count(h.progress.String(), "poll "); polls != 1 {
		t.Errorf("want one poll, got %d:\n%s", polls, h.progress.String())
	}
}

func Test_Wait_forkAwaitingApprovalTimesOut(t *testing.T) {
	// The fork's run completed with action_required and produced no check
	// runs: nothing is red, nothing is green, and the timeout names the run.
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{checkRuns()}
	gh[actsPath] = []sequence.Response{body(map[string]any{"total_count": 1, "workflow_runs": []any{
		map[string]any{"id": 7, "name": "CI", "status": "completed", "conclusion": "action_required", "html_url": "https://github.com/o/r/actions/runs/7"},
	}})}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitTimeout {
		t.Fatalf("want exit 2, got %d %v", exitCode(err), err)
	}
	if want := []string{"actions run CI (awaiting approval)"}; !cmp.Equal(want, result.Unfinished) {
		t.Errorf("unfinished: want %q, got %q", want, result.Unfinished)
	}
	if h.cond.Replayed() == 0 {
		t.Error("want the repeated polls answered from the ETag cache")
	}
}

func Test_Wait_forkAwaitingApprovalOutranksRequiredMissing(t *testing.T) {
	// The base requires contexts the approved run would report under. While
	// the run awaits approval the outcome is not known: the timeout is 2,
	// not 4, and unfinished names the run and the absent contexts.
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{checkRuns()}
	gh[actsPath] = []sequence.Response{body(map[string]any{"total_count": 1, "workflow_runs": []any{
		map[string]any{"id": 7, "name": "CI", "status": "completed", "conclusion": "action_required", "html_url": "https://github.com/o/r/actions/runs/7"},
	}})}
	gh["GET /repos/o/r/branches/main/protection"] = []sequence.Response{body(map[string]any{
		"required_status_checks": map[string]any{"strict": true, "contexts": []any{"go-build"}},
	})}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitTimeout || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("want exit 2 at the timeout, got %d %v", exitCode(err), err)
	}
	if want := []string{"actions run CI (awaiting approval)", "required context go-build (absent)"}; !cmp.Equal(want, result.Unfinished) {
		t.Errorf("unfinished: want %q, got %q", want, result.Unfinished)
	}
}

func Test_Wait_requiredContextNeverReported(t *testing.T) {
	// Every check and run of the head has finished and two required contexts
	// are absent: exit 4 at the first poll, not at the timeout.

	gh := gitHubGreen()
	gh["GET /repos/o/r/branches/main/protection"] = []sequence.Response{body(map[string]any{
		"required_status_checks": map[string]any{"strict": true, "contexts": []any{"go-build", "ci/circleci: test"}},
	})}
	gh["GET /repos/o/r/rules/branches/main"] = []sequence.Response{body([]any{
		map[string]any{"type": "required_status_checks", "parameters": map[string]any{"required_status_checks": []any{map[string]any{"context": "lint"}}}},
	})}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRequiredMissing {
		t.Fatalf("want exit 4, got %d %v", exitCode(err), err)
	}
	for _, name := range []string{"ci/circleci: test", "lint"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("reason names %q: %v", name, err)
		}
	}
	if want := []string{"required context ci/circleci: test (absent)", "required context lint (absent)"}; !cmp.Equal(want, result.Unfinished) {
		t.Errorf("unfinished: want %q, got %q", want, result.Unfinished)
	}
	if len(result.Checks) != 1 || !result.Checks[0].Required {
		t.Errorf("go-build is flagged required: %+v", result.Checks)
	}
	if polls := strings.Count(h.progress.String(), "poll "); polls != 1 {
		t.Errorf("want one poll, got %d:\n%s", polls, h.progress.String())
	}
}

func Test_Wait_headChangeIsWarned(t *testing.T) {
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{checkRuns(run("go-build", "in_progress", ""))}
	newHead := pull("clean", map[string]any{"head": map[string]any{"sha": "def456", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}}})
	gh[pullPath] = []sequence.Response{pull("clean", nil), newHead}
	gh["GET /repos/o/r/commits/def456/check-runs"] = []sequence.Response{checkRuns(run("go-build", "completed", "success"))}
	gh["GET /repos/o/r/commits/def456/status"] = []sequence.Response{noStatuses}
	gh["GET /repos/o/r/actions/runs?head_sha=def456"] = []sequence.Response{noRuns}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green on the new head, got %v", err)
	}
	if result.HeadSHA != "def456" || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "abc123 to def456") {
		t.Errorf("want the new head and one warning, got %s %q", result.HeadSHA, result.Warnings)
	}
}

func Test_Wait_toolingErrorIsNotAVerdict(t *testing.T) {
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{{Status: 500, Body: map[string]any{"message": "boom"}}}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	var coder agentcli.ExitCoder
	if err == nil || errors.As(err, &coder) {
		t.Fatalf("want a plain error (exit 7 in the envelope), got %v", err)
	}
}
