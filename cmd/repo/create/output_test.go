package create

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type outputTransport func(*http.Request) (*http.Response, error)

func (f outputTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// outputCommand exercises the public command with a synthetic token and an
// entirely in-memory GitHub API. No request can reach a network or keychain.
func outputCommand(t *testing.T, stdout io.Writer, format string, dryRun bool, extraArgs ...string) (error, []string) {
	t.Helper()
	t.Setenv("DEVCTL_CREATE_OUTPUT_TEST_TOKEN", "synthetic-token")
	t.Setenv("CI", "true")
	var writes []string
	previous := http.DefaultTransport
	http.DefaultTransport = outputTransport(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, `{}`
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/giantswarm/github/contents/repositories/team-fixture.yaml":
			body = `{"type":"file","sha":"team-sha","encoding":"base64","content":""}`
		case "GET /repos/giantswarm/github/contents/.github/repositories.schema.json":
			// The real creation rules still run; only the schema read is synthetic.
			body = `{"type":"file","encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(`{"type":"array","items":{"type":"object"}}`)) + `"}`
		case "GET /user":
			body = `{"login":"fixture-user"}`
		case "GET /orgs/giantswarm/teams/team-fixture", "GET /orgs/giantswarm/teams/team-planeteers":
			body = `{"slug":"fixture-team"}`
		case "GET /orgs/giantswarm/teams/team-fixture/memberships/fixture-user", "GET /orgs/giantswarm/teams/team-planeteers/memberships/fixture-user":
			body = `{"state":"active"}`
		case "GET /repos/giantswarm/fixture-repo":
			status = http.StatusNotFound
		case "GET /repos/giantswarm/github/git/ref/heads/main":
			body = `{"object":{"sha":"main-sha"}}`
		case "POST /repos/giantswarm/github/git/refs":
			writes = append(writes, "branch")
			status, body = http.StatusCreated, `{"ref":"refs/heads/repo-create/fixture-repo"}`
		case "PUT /repos/giantswarm/github/contents/repositories/team-fixture.yaml":
			writes = append(writes, "commit")
		case "POST /repos/giantswarm/github/pulls":
			writes = append(writes, "pull request")
			status, body = http.StatusCreated, `{"number":6179,"html_url":"https://github.com/giantswarm/github/pull/6179"}`
		default:
			t.Fatalf("unexpected offline request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{
			StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cmd, err := New(Config{Logger: logger, Stdout: stdout, Stderr: io.Discard})
	require.NoError(t, err)
	args := []string{
		"--github-token-envvar", "DEVCTL_CREATE_OUTPUT_TEST_TOKEN",
		"--team", "fixture", "--name", "fixture-repo", "--component-type", "service",
		"--flavour", "generic", "--language", "generic", "--output", format,
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, extraArgs...)
	cmd.SetArgs(args)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err = cmd.Execute()
	return err, writes
}

type outputFailure struct {
	marker string
	err    error
}

func (w outputFailure) Write(p []byte) (int, error) {
	if at := bytes.Index(p, []byte(w.marker)); at >= 0 {
		return at, w.err
	}
	return len(p), nil
}

func TestCommandOutputFailure(t *testing.T) {
	for _, tc := range []struct {
		name, format, marker string
		dryRun               bool
		wantWrites           []string
	}{
		{name: "text immediate failure", format: outputText, marker: "giantswarm/"},
		{name: "text preview before writes", format: outputText, marker: "fixture-repo"},
		{name: "text dry run receipt", format: outputText, marker: "dry run:", dryRun: true},
		{name: "text pull request receipt", format: outputText, marker: "pull request:", wantWrites: []string{"branch", "commit", "pull request"}},
		{name: "text watch handoff", format: outputText, marker: "next:", wantWrites: []string{"branch", "commit", "pull request"}},
		{name: "JSON dry run", format: outputJSON, marker: `"dryRun"`, dryRun: true},
		{name: "JSON pull request receipt", format: outputJSON, marker: `"pullRequest"`, wantWrites: []string{"branch", "commit", "pull request"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("synthetic output failure")
			err, writes := outputCommand(t, outputFailure{marker: tc.marker, err: failure}, tc.format, tc.dryRun)
			require.ErrorIs(t, err, failure)
			require.Equal(t, tc.wantWrites, writes, "output failures must not start or repeat remote writes")
		})
	}
}

func TestCommandRefusalOutputFailure(t *testing.T) {
	failure := errors.New("synthetic output failure")
	err, writes := outputCommand(t, outputFailure{marker: `"dryRun"`, err: failure}, outputJSON, false, "--name", "Bad_Name")
	require.True(t, IsRefused(err), "keep the refusal when its JSON diagnostic cannot be written: %v", err)
	require.Empty(t, writes)
}

func TestCommandOutputSuccess(t *testing.T) {
	for _, format := range []string{outputText, outputJSON} {
		t.Run(format, func(t *testing.T) {
			var stdout bytes.Buffer
			err, writes := outputCommand(t, &stdout, format, false)
			require.NoError(t, err)
			require.Equal(t, []string{"branch", "commit", "pull request"}, writes)
			const next = "devctl repo watch giantswarm/fixture-repo --pull-request 6179"
			if format == outputText {
				require.Contains(t, stdout.String(), "verdict:    accepted")
				require.Contains(t, stdout.String(), "pull request: https://github.com/giantswarm/github/pull/6179 (opened)")
				require.Contains(t, stdout.String(), "next: "+next)
				return
			}
			var got output
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
			require.True(t, got.DryRun.Accepted)
			require.Equal(t, "https://github.com/giantswarm/github/pull/6179", got.PullRequest)
			require.Equal(t, next, got.Next)
		})
	}
}
