package prwait

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func ptr[T any](v T) *T { return &v }

func checkRun(id int64, name, status, conclusion string, started time.Time) *github.CheckRun {
	run := &github.CheckRun{
		ID:        &id,
		Name:      &name,
		Status:    &status,
		StartedAt: &github.Timestamp{Time: started},
		HTMLURL:   ptr("https://github.com/o/r/runs/" + name),
	}
	if conclusion != "" {
		run.Conclusion = &conclusion
	}
	return run
}

func status(context, state string) *github.RepoStatus {
	return &github.RepoStatus{Context: &context, State: &state, TargetURL: ptr("https://ci/" + context)}
}

func workflowRun(id int64, name, status, conclusion string) *github.WorkflowRun {
	run := &github.WorkflowRun{ID: &id, Name: &name, Status: &status, HTMLURL: ptr("https://github.com/o/r/actions/runs/" + name)}
	if conclusion != "" {
		run.Conclusion = &conclusion
	}
	return run
}

func circle(state string, workflows ...circleciclient.Workflow) *circleSnapshot {
	return &circleSnapshot{
		project:   "github/o/r",
		pipeline:  &circleciclient.Pipeline{ID: "p1", Number: 7, State: state},
		workflows: workflows,
	}
}

func Test_evaluate(t *testing.T) {
	t0 := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	testCases := []struct {
		name           string
		snapshot       snapshot
		wantGreen      bool
		wantRed        []string
		wantUnfinished []string
		wantMissing    []string
		// wantNeverReported: settled with a required context absent, exit 4
		// at this poll.
		wantNeverReported bool
		wantChecks        []Check
		wantActions       []ActionRun
		wantCircleCI      *CircleCI
		skipFieldChecks   bool
	}{
		{
			name: "every check run and status completed green, no runs open: green",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{checkRun(1, "go-build", "completed", "success", t0)},
				statuses:  []*github.RepoStatus{status("ci/circleci: test", "success")},
				runs:      []*github.WorkflowRun{workflowRun(10, "CI", "completed", "success")},
				required:  []string{"go-build"},
			},
			wantGreen: true,
			wantChecks: []Check{
				{Name: "ci/circleci: test", Source: SourceStatus, Status: "completed", Conclusion: "success", URL: "https://ci/ci/circleci: test"},
				{Name: "go-build", Source: SourceCheckRun, Status: "completed", Conclusion: "success", URL: "https://github.com/o/r/runs/go-build", Required: true},
			},
			wantActions: []ActionRun{{Name: "CI", RunID: 10, Status: "completed", Conclusion: "success", URL: "https://github.com/o/r/actions/runs/CI"}},
		},
		{
			name: "the stage gap: checks green, the CircleCI workflow behind requires still running",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{checkRun(1, "go-build", "completed", "success", t0)},
				circleci:  circle("created", circleciclient.Workflow{ID: "w1", Name: "build", Status: "running"}),
			},
			wantUnfinished: []string{"circleci workflow build (running)"},
			wantChecks:     []Check{{Name: "go-build", Source: SourceCheckRun, Status: "completed", Conclusion: "success", URL: "https://github.com/o/r/runs/go-build"}},
			wantActions:    []ActionRun{},
			wantCircleCI: &CircleCI{PipelineID: "p1", PipelineNumber: 7, Workflows: []Workflow{
				{Name: "build", Status: "running", URL: "https://app.circleci.com/pipelines/github/o/r/7/workflows/w1"},
			}},
		},
		{
			name: "the fork awaiting approval: the run completed with action_required and produced no check runs",
			snapshot: snapshot{
				runs: []*github.WorkflowRun{workflowRun(10, "CI", "completed", "action_required")},
			},
			wantUnfinished:  []string{"actions run CI (awaiting approval)"},
			skipFieldChecks: true,
		},
		{
			name: "the retitled pull request: the stale failed title run is replaced by the newer success",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{
					checkRun(1, "pr-title", "completed", "failure", t0),
					checkRun(2, "pr-title", "completed", "success", t1),
				},
				runs: []*github.WorkflowRun{
					workflowRun(10, "PR title", "completed", "failure"),
					workflowRun(11, "PR title", "completed", "success"),
				},
			},
			wantGreen:  true,
			wantChecks: []Check{{Name: "pr-title", Source: SourceCheckRun, Status: "completed", Conclusion: "success", URL: "https://github.com/o/r/runs/pr-title"}},
			wantActions: []ActionRun{
				{Name: "PR title", RunID: 11, Status: "completed", Conclusion: "success", URL: "https://github.com/o/r/actions/runs/PR title"},
			},
		},
		{
			name: "a red CircleCI workflow: red at once although a rerun of another workflow still runs",
			snapshot: snapshot{
				circleci: circle("created",
					circleciclient.Workflow{ID: "w1", Name: "build", Status: "failed"},
					circleciclient.Workflow{ID: "w2", Name: "release", Status: "running"},
				),
			},
			wantRed:         []string{"circleci workflow build failed"},
			wantUnfinished:  []string{"circleci workflow release (running)"},
			skipFieldChecks: true,
		},
		{
			name: "a rerun CircleCI workflow: the newest run of the name counts, the failed one does not",
			snapshot: snapshot{
				circleci: circle("created",
					circleciclient.Workflow{ID: "w1", Name: "build", Status: "failed", CreatedAt: t0},
					circleciclient.Workflow{ID: "w2", Name: "build", Status: "success", CreatedAt: t1},
				),
			},
			wantGreen:       true,
			skipFieldChecks: true,
		},
		{
			name: "no CircleCI pipeline for the head revision yet",
			snapshot: snapshot{
				headSHA:  "abc123",
				circleci: &circleSnapshot{project: "github/o/r"},
			},
			wantUnfinished:  []string{"circleci pipeline for abc123 (absent)"},
			skipFieldChecks: true,
		},
		{
			name: "an errored CircleCI pipeline is red",
			snapshot: snapshot{
				circleci: circle("errored"),
			},
			wantRed:         []string{"circleci pipeline 7 errored"},
			skipFieldChecks: true,
		},
		{
			name: "a not_run workflow is skipped, not unfinished",
			snapshot: snapshot{
				circleci: circle("created", circleciclient.Workflow{ID: "w1", Name: "nightly", Status: "not_run"}),
			},
			wantGreen:       true,
			skipFieldChecks: true,
		},
		{
			name: "a failed check run is red; the queued one is still named",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{
					checkRun(1, "go-test", "completed", "failure", t0),
					checkRun(2, "lint", "queued", "", t0),
				},
			},
			wantRed:         []string{"check go-test concluded failure"},
			wantUnfinished:  []string{"check lint (queued)"},
			skipFieldChecks: true,
		},
		{
			name: "a check run that needs a human waits; neutral and skipped pass",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{
					checkRun(1, "deploy", "completed", "action_required", t0),
					checkRun(2, "docs", "completed", "neutral", t0),
					checkRun(3, "optional", "completed", "skipped", t0),
				},
			},
			wantUnfinished:  []string{"check deploy (action_required)"},
			skipFieldChecks: true,
		},
		{
			name: "a failed status is red, a pending one waits",
			snapshot: snapshot{
				statuses: []*github.RepoStatus{status("ci/a", "error"), status("ci/b", "pending")},
			},
			wantRed:        []string{"status ci/a is error"},
			wantUnfinished: []string{"status ci/b (pending)"},
			wantChecks: []Check{
				{Name: "ci/a", Source: SourceStatus, Status: "completed", Conclusion: "error", URL: "https://ci/ci/a"},
				{Name: "ci/b", Source: SourceStatus, Status: "pending", URL: "https://ci/ci/b"},
			},
			wantActions: []ActionRun{},
		},
		{
			name: "every check and run finished, a required context absent: never reported, known without a timeout",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{checkRun(1, "go-build", "completed", "success", t0)},
				runs:      []*github.WorkflowRun{workflowRun(10, "CI", "completed", "success")},
				required:  []string{"ci/circleci: test", "go-build"},
			},
			wantUnfinished:    []string{"required context ci/circleci: test (absent)"},
			wantMissing:       []string{"ci/circleci: test"},
			wantNeverReported: true,
			skipFieldChecks:   true,
		},
		{
			name: "a run awaiting approval next to an absent required context: pending, the run may be what reports it",
			snapshot: snapshot{
				runs:     []*github.WorkflowRun{workflowRun(10, "CI", "completed", "action_required")},
				required: []string{"go-build"},
			},
			wantUnfinished:  []string{"actions run CI (awaiting approval)", "required context go-build (absent)"},
			wantMissing:     []string{"go-build"},
			skipFieldChecks: true,
		},
		{
			name: "a queued check next to an absent required context: pending, not never reported",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{checkRun(1, "go-build", "queued", "", t0)},
				required:  []string{"go-build", "lint"},
			},
			wantUnfinished:  []string{"check go-build (queued)", "required context lint (absent)"},
			wantMissing:     []string{"lint"},
			skipFieldChecks: true,
		},
		{
			name: "an open Actions run keeps the wait going although its check runs read complete",
			snapshot: snapshot{
				checkRuns: []*github.CheckRun{checkRun(1, "go-build", "completed", "success", t0)},
				runs:      []*github.WorkflowRun{workflowRun(10, "CI", "in_progress", "")},
			},
			wantUnfinished:  []string{"actions run CI (in_progress)"},
			skipFieldChecks: true,
		},
		{
			name:           "nothing reported at all is not green: an empty head has no verdict yet",
			snapshot:       snapshot{circleci: circle("created")},
			wantUnfinished: []string{"circleci pipeline 7 (no workflows yet)"},
			wantChecks:     []Check{},
			wantActions:    []ActionRun{},
			wantCircleCI:   &CircleCI{PipelineID: "p1", PipelineNumber: 7, Workflows: []Workflow{}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := evaluate(tc.snapshot)
			if e.green() != tc.wantGreen {
				t.Errorf("green: want %v, got %v (red %q, unfinished %q)", tc.wantGreen, e.green(), e.red, e.unfinished)
			}
			if diff := cmp.Diff(tc.wantRed, e.red, cmp.Transformer("nilToEmpty", nilToEmpty)); diff != "" {
				t.Errorf("red:\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantUnfinished, e.unfinished, cmp.Transformer("nilToEmpty", nilToEmpty)); diff != "" {
				t.Errorf("unfinished:\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantMissing, e.requiredMissing, cmp.Transformer("nilToEmpty", nilToEmpty)); diff != "" {
				t.Errorf("requiredMissing:\n%s", diff)
			}
			if e.neverReported() != tc.wantNeverReported {
				t.Errorf("neverReported: want %v, got %v (settled %v, unfinished %q)", tc.wantNeverReported, e.neverReported(), e.settled, e.unfinished)
			}
			if tc.skipFieldChecks {
				return
			}
			if diff := cmp.Diff(tc.wantChecks, e.checks); diff != "" {
				t.Errorf("checks:\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantActions, e.actions); diff != "" {
				t.Errorf("actions:\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantCircleCI, e.circleci); diff != "" {
				t.Errorf("circleci:\n%s", diff)
			}
		})
	}
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func Test_evaluate_emptyHeadIsGreen(t *testing.T) {
	// A head without any check, status, run or CircleCI is green: there is
	// nothing to wait for, and a repository without CI merges on review alone.
	// The required contexts of a protected base are what make such a head
	// wait instead.
	if e := evaluate(snapshot{}); !e.green() {
		t.Errorf("want green, got red %q unfinished %q", e.red, e.unfinished)
	}
	if e := evaluate(snapshot{required: []string{"go-build"}}); e.green() {
		t.Error("want a wait for the required context, got green")
	}
}
