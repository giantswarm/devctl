package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// sequence answers the watch tool with one payload per call, the last one
// repeating, and records the arguments.
type sequence struct {
	answers []string
	calls   int
	args    []map[string]any
}

func (s *sequence) Call(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	s.args = append(s.args, args)
	n := s.calls
	s.calls++
	if n >= len(s.answers) {
		n = len(s.answers) - 1
	}
	return json.RawMessage(s.answers[n]), nil
}

func newRunner(answers ...string) (*runner, *sequence, *bytes.Buffer) {
	seq := &sequence{answers: answers}
	var out bytes.Buffer
	r := &runner{
		flag:   &flag{Flags: client.Flags{Output: client.OutputText}, PullRequest: 4711, Timeout: time.Minute},
		logger: logrus.New(),
		stdout: &out,
		stderr: &bytes.Buffer{},
		open: func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: seq, Endpoint: "https://muster.example/mcp"}, nil
		},
	}
	return r, seq, &out
}

const (
	pending      = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"scaffolded","at":"2026-09-22T10:00:05Z","seconds":5}],"changed":true,"ready":false,"pending":"declared","pendingReason":"the pull request is not open yet","waited":3}`
	ready        = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"scaffolded","at":"2026-09-22T10:00:05Z","seconds":5},{"name":"declared","at":"2026-09-22T10:00:09Z","seconds":4},{"name":"merged","at":"2026-09-22T10:01:00Z","seconds":51},{"name":"setUp","at":"2026-09-22T10:01:40Z","seconds":40},{"name":"released","at":"2026-09-22T10:03:30Z","seconds":110}],"changed":false,"ready":true,"release":{"tag":"v0.1.0","url":"https://github.com/giantswarm/new/releases/tag/v0.1.0"},"findings":[{"kind":"default-icon","message":"the default icon","fix":"replace it"}],"waited":1}`
	setUpPending = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"scaffolded","at":"2026-09-22T10:00:05Z","seconds":5},{"name":"declared","at":"2026-09-22T10:00:09Z","seconds":4},{"name":"merged","at":"2026-09-22T10:01:00Z","seconds":51}],"changed":true,"ready":false,"pending":"setUp","waited":3}`
	readyNoCI    = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"scaffolded","at":"2026-09-22T10:00:05Z","seconds":5},{"name":"declared","at":"2026-09-22T10:00:09Z","seconds":4},{"name":"merged","at":"2026-09-22T10:01:00Z","seconds":51},{"name":"setUp","at":"2026-09-22T10:01:40Z","seconds":40}],"changed":true,"ready":true,"withoutCI":true,"waited":40}`
	failed       = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0}],"ready":false,"failure":{"phase":"released","reason":"ci/circleci: push-to-registries failed"},"waited":2}`
)

// The phases are printed once each as they complete, the pending reason
// when it changes, and the closing line names the release and the findings.
func TestRunReady(t *testing.T) {
	r, seq, out := newRunner(pending, ready)
	require.NoError(t, r.run(context.Background(), "new"))
	require.Equal(t, 2, seq.calls)
	require.Equal(t, "new", seq.args[0]["repository"])
	require.Equal(t, 4711, seq.args[0]["pullRequest"])
	require.Equal(t, 60, seq.args[0]["timeout"], "the ask is bounded by the remaining timeout")

	text := out.String()
	require.Equal(t, 1, countOf(text, "  created    2026-09-22T10:00:00Z (+0s)"))
	require.Equal(t, 1, countOf(text, "  scaffolded 2026-09-22T10:00:05Z (+5s)"))
	require.Contains(t, text, "waiting for declared: the pull request is not open yet")
	require.Contains(t, text, "  released   2026-09-22T10:03:30Z (+110s)")
	require.Contains(t, text, "ready: https://github.com/giantswarm/new (release v0.1.0, https://github.com/giantswarm/new/releases/tag/v0.1.0)")
	require.Contains(t, text, "  default-icon: the default icon -- fix: replace it")
}

// A repository without CI is ready once set up: the watch ends with no
// error and no released phase, saying there is no release to wait for.
func TestRunReadyWithoutCI(t *testing.T) {
	r, seq, out := newRunner(setUpPending, readyNoCI)
	require.NoError(t, r.run(context.Background(), "new"))
	require.Equal(t, 2, seq.calls)

	text := out.String()
	require.Contains(t, text, "waiting for setUp")
	require.Contains(t, text, "  setUp      2026-09-22T10:01:40Z (+40s)")
	require.NotContains(t, text, "released")
	require.Contains(t, text, "ready: https://github.com/giantswarm/new (no CI: set up, no release to wait for)")
}

// A failed phase ends the watch with an error naming it.
func TestRunFailed(t *testing.T) {
	r, _, out := newRunner(failed)
	err := r.run(context.Background(), "new")
	require.True(t, IsFailed(err), err)
	require.Contains(t, err.Error(), "failed at released: ci/circleci: push-to-registries failed")
	require.Contains(t, out.String(), "failed at released")
}

// In JSON mode the last answer is printed whole.
func TestRunJSON(t *testing.T) {
	r, _, out := newRunner(pending, ready)
	r.flag.Output = client.OutputJSON
	require.NoError(t, r.run(context.Background(), "new"))
	var doc manager.Watch
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.True(t, doc.Ready)
	require.Len(t, doc.Phases, 6)
}

func countOf(text, want string) int {
	return bytes.Count([]byte(text), []byte(want))
}

// Once the reconciler set the repository up, the watch sets its CircleCI
// pipeline up as the person, once, and goes on waiting for the release.
func TestRunSetsCircleCIUpOnceSetUp(t *testing.T) {
	r, _, out := newRunner(pending, setUpPending, setUpDone, setUpDone, ready)
	r.flag.CircleCI = true
	var calls []string
	r.setUpCircleCI = func(_ context.Context, owner, repo string, progress func(string, ...any)) (tagWait, error) {
		calls = append(calls, owner+"/"+repo)
		progress("created the GitHub App pipeline %s", repo+"-app")
		return nil, nil
	}
	require.NoError(t, r.run(context.Background(), "new"))
	require.Equal(t, []string{"giantswarm/new"}, calls)
	require.Contains(t, out.String(), "  circleci   created the GitHub App pipeline new-app")
}

// The CircleCI step is skipped for a repository without CI: it is ready
// once set up.
func TestRunCircleCISkippedWithoutCI(t *testing.T) {
	r, _, _ := newRunner(setUpPending, readyNoCI)
	r.flag.CircleCI = true
	r.setUpCircleCI = func(context.Context, string, string, func(string, ...any)) (tagWait, error) {
		t.Fatal("no CircleCI set-up for a repository without CI")
		return nil, nil
	}
	require.NoError(t, r.run(context.Background(), "new"))
}

// A CircleCI set-up that fails ends the watch, naming the person's fix.
func TestRunCircleCIFailure(t *testing.T) {
	r, seq, _ := newRunner(setUpDone, ready)
	r.flag.CircleCI = true
	r.setUpCircleCI = func(context.Context, string, string, func(string, ...any)) (tagWait, error) {
		return nil, errors.New("CircleCI authentication required")
	}
	err := r.run(context.Background(), "giantswarm/new")
	require.Error(t, err)
	require.Contains(t, err.Error(), "devctl repo follow-circleci giantswarm/new")
	require.Contains(t, err.Error(), "devctl auth login --circleci-only")
	require.Equal(t, 1, seq.calls, "the watch ends at the failure")
}

// setUpDone is a watch past the set-up, waiting for the release.
const setUpDone = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"setUp","at":"2026-09-22T10:01:40Z","seconds":40}],"changed":true,"ready":false,"pending":"released","waited":3}`

// releasedMissed is the answer for a first tag cut before the pipeline
// existed: set up, and the released phase failed on the reconciler's
// missed-tag-build.
// The reason is worded as the manager words it, from the finding's message
// and fix; the finding carries the kind.
const releasedMissed = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"scaffolded","at":"2026-09-22T10:00:05Z","seconds":5},{"name":"declared","at":"2026-09-22T10:00:09Z","seconds":4},{"name":"merged","at":"2026-09-22T10:01:00Z","seconds":51},{"name":"setUp","at":"2026-09-22T10:01:40Z","seconds":40}],"ready":false,"failure":{"phase":"released","reason":"release v0.1.0 of giantswarm/new carries no CircleCI status of a build started after the release: nothing was built or published for the tag — ` + "`devctl repo follow-circleci giantswarm/new`" + `: it runs the tag on the GitHub App pipeline as you, creating the pipeline first when there is none; or cut the next tag (https://github.com/giantswarm/github/actions/runs/1)"},"findings":[{"kind":"missed-tag-build","message":"release v0.1.0 of giantswarm/new carries no CircleCI status of a build started after the release: nothing was built or published for the tag","fix":"` + "`devctl repo follow-circleci giantswarm/new`" + `: it runs the tag on the GitHub App pipeline as you, creating the pipeline first when there is none; or cut the next tag"},{"kind":"default-icon","message":"the default icon","fix":"replace it"}],"waited":1}`

// The first tag's missed build is the answer the CircleCI set-up exists
// for: the watch sets CircleCI up on it, and ends on the release-tag run the
// set-up started, read as the person, not on the manager's stale verdict.
func TestRunEndsOnTheTagRunTheSetUpStarted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		green bool
	}{{"green", true}, {"red", false}} {
		t.Run(tc.name, func(t *testing.T) {
			r, seq, out := newRunner(releasedMissed)
			r.flag.CircleCI = true
			r.setUpCircleCI = func(context.Context, string, string, func(string, ...any)) (tagWait, error) {
				return func(context.Context) ([]circleciapp.Run, error) {
					outcome := &circleciapp.Outcome{Green: tc.green}
					if !tc.green {
						outcome.Failed = []string{"build (failed)"}
					}
					return []circleciapp.Run{{Ref: circleciclient.Ref{Tag: "v0.1.0"}, Pipeline: circleciclient.Pipeline{Number: 3}, Outcome: outcome}}, nil
				}, nil
			}
			err := r.run(context.Background(), "new")
			require.Equal(t, 1, seq.calls, "the manager is not asked again")
			if tc.green {
				require.NoError(t, err)
				require.Contains(t, out.String(), "ready: https://github.com/giantswarm/new (the release build the CircleCI set-up started is green)")
				require.NotContains(t, out.String(), "failed at released", "the manager's stale verdict is not printed")
				require.Contains(t, out.String(), "  setUp      2026-09-22T10:01:40Z", "the phases reached are")
				return
			}
			require.True(t, IsFailed(err), err)
			require.Contains(t, err.Error(), "pipeline 3 (tag v0.1.0): build (failed)")
		})
	}
}

// Without the set-up, a released failure still ends the watch.
func TestRunReleasedFailureWithoutCircleCI(t *testing.T) {
	r, _, _ := newRunner(releasedMissed)
	err := r.run(context.Background(), "new")
	require.True(t, IsFailed(err), err)
}

// In JSON mode the document follows the run: ready on exit 0, the red
// pipeline as the failure otherwise; never the manager's stale verdict.
func TestRunTagRunJSON(t *testing.T) {
	r, _, out := newRunner(releasedMissed)
	r.flag.CircleCI = true
	r.flag.Output = client.OutputJSON
	r.setUpCircleCI = func(context.Context, string, string, func(string, ...any)) (tagWait, error) {
		return func(context.Context) ([]circleciapp.Run, error) {
			return []circleciapp.Run{{Ref: circleciclient.Ref{Tag: "v0.1.0"}, Outcome: &circleciapp.Outcome{Green: true}}}, nil
		}, nil
	}
	require.NoError(t, r.run(context.Background(), "new"))
	var doc manager.Watch
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.True(t, doc.Ready)
	require.Nil(t, doc.Failure)
	require.Len(t, doc.Findings, 1, "the stale missed-tag-build is dropped, the rest stays")
	require.Equal(t, "default-icon", string(doc.Findings[0].Kind))
}

// A tag run that cannot be read is pending in JSON, not failed on the
// reconciler's earlier verdict, and the stale finding is gone.
func TestRunTagRunReadErrorJSON(t *testing.T) {
	r, _, out := newRunner(releasedMissed)
	r.flag.CircleCI = true
	r.flag.Output = client.OutputJSON
	r.setUpCircleCI = func(context.Context, string, string, func(string, ...any)) (tagWait, error) {
		return func(context.Context) ([]circleciapp.Run, error) { return nil, errors.New("CircleCI 502") }, nil
	}
	err := r.run(context.Background(), "new")
	require.Error(t, err)
	require.Contains(t, err.Error(), "reading the release build the CircleCI set-up started")
	var doc manager.Watch
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Nil(t, doc.Failure)
	require.Equal(t, "released", doc.Pending)
	require.Contains(t, doc.PendingReason, "https://app.circleci.com/pipelines/github/new")
	require.Len(t, doc.Findings, 1)
	require.Equal(t, "default-icon", string(doc.Findings[0].Kind))
}

// A red tag run fails released with the run's jobs, in JSON too, and the
// stale missed-tag-build is not offered as the cause.
func TestRunTagRunRedJSON(t *testing.T) {
	r, _, out := newRunner(releasedMissed)
	r.flag.CircleCI = true
	r.flag.Output = client.OutputJSON
	r.setUpCircleCI = func(context.Context, string, string, func(string, ...any)) (tagWait, error) {
		return func(context.Context) ([]circleciapp.Run, error) {
			return []circleciapp.Run{{Ref: circleciclient.Ref{Tag: "v0.1.0"}, Pipeline: circleciclient.Pipeline{Number: 4}, Outcome: &circleciapp.Outcome{Failed: []string{"build (failed)"}}}}, nil
		}, nil
	}
	require.True(t, IsFailed(r.run(context.Background(), "new")))
	var doc manager.Watch
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Contains(t, doc.Failure.Reason, "pipeline 4 (tag v0.1.0): build (failed)")
	require.Len(t, doc.Findings, 1)
	require.Empty(t, doc.PendingReason)
}

// A timeout while the tag run is still going prints the pending document.
func TestRunTagRunTimeoutJSON(t *testing.T) {
	r, _, out := newRunner(releasedMissed)
	r.flag.CircleCI = true
	r.flag.Output = client.OutputJSON
	r.flag.Timeout = 50 * time.Millisecond
	r.setUpCircleCI = func(context.Context, string, string, func(string, ...any)) (tagWait, error) {
		return func(ctx context.Context) ([]circleciapp.Run, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, nil
	}
	err := r.run(context.Background(), "new")
	require.True(t, IsTimeout(err), err)
	var doc manager.Watch
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Equal(t, "released", doc.Pending)
}
