package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
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
	pending = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"scaffolded","at":"2026-09-22T10:00:05Z","seconds":5}],"changed":true,"ready":false,"pending":"declared","pendingReason":"the pull request is not open yet","waited":3}`
	ready   = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0},{"name":"scaffolded","at":"2026-09-22T10:00:05Z","seconds":5},{"name":"declared","at":"2026-09-22T10:00:09Z","seconds":4},{"name":"merged","at":"2026-09-22T10:01:00Z","seconds":51},{"name":"setUp","at":"2026-09-22T10:01:40Z","seconds":40},{"name":"released","at":"2026-09-22T10:03:30Z","seconds":110}],"changed":false,"ready":true,"release":{"tag":"v0.1.0","url":"https://github.com/giantswarm/new/releases/tag/v0.1.0"},"findings":[{"kind":"default-icon","message":"the default icon","fix":"replace it"}],"waited":1}`
	failed  = `{"repository":"https://github.com/giantswarm/new","phases":[{"name":"created","at":"2026-09-22T10:00:00Z","seconds":0}],"ready":false,"failure":{"phase":"released","reason":"ci/circleci: push-to-registries failed"},"waited":2}`
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
