package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/giantswarm/microerror"
)

type authErr struct{}

func (authErr) Error() string        { return "GitHub authentication required: run `devctl auth login`." }
func (authErr) ExitCode() int        { return ExitAuthRequired }
func (authErr) ExitVerdict() Verdict { return VerdictAuthRequired }

func TestEnvelopeFinish(t *testing.T) {
	started := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	finished := started.Add(2 * time.Second)

	cases := []struct {
		name    string
		err     error
		code    int
		verdict Verdict
		reason  string
	}{
		{name: "ok", err: nil, code: ExitOK, verdict: VerdictGreen, reason: ""},
		{name: "exit coder", err: authErr{}, code: ExitAuthRequired, verdict: VerdictAuthRequired, reason: authErr{}.Error()},
		{name: "masked exit coder", err: microerror.Mask(authErr{}), code: ExitAuthRequired, verdict: VerdictAuthRequired, reason: microerror.Mask(authErr{}).Error()},
		{name: "wrapped exit error", err: fmt.Errorf("context: %w", NewExitError(ExitRefused, VerdictRefused, "not yours")), code: ExitRefused, verdict: VerdictRefused, reason: "context: not yours"},
		{name: "plain error is tooling", err: errors.New("boom"), code: ExitUsage, verdict: VerdictUsage, reason: "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEnvelope("auth status", started)
			e.Warn("")
			e.Warn("careful")
			e.Finish(finished, VerdictGreen, tc.err)
			if e.ExitCode != tc.code || e.Verdict != tc.verdict || e.Reason != tc.reason {
				t.Fatalf("got %d/%s/%q, want %d/%s/%q", e.ExitCode, e.Verdict, e.Reason, tc.code, tc.verdict, tc.reason)
			}
			if e.StartedAt != started || e.FinishedAt != finished || e.SchemaVersion != SchemaVersion {
				t.Fatalf("timestamps or schema wrong: %+v", e)
			}
			if len(e.Warnings) != 1 || e.Warnings[0] != "careful" {
				t.Fatalf("warnings: %v", e.Warnings)
			}
			if Exit(tc.err) != tc.code {
				t.Fatalf("Exit(%v) = %d, want %d", tc.err, Exit(tc.err), tc.code)
			}
			err := e.Err()
			if tc.code == ExitOK {
				if err != nil {
					t.Fatalf("Err() = %v for exit 0", err)
				}
				return
			}
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != tc.code || exitErr.Verdict != tc.verdict {
				t.Fatalf("Err() = %#v", err)
			}
		})
	}
}

func TestEmitEmbedsEnvelope(t *testing.T) {
	type document struct {
		Envelope
		Repository string `json:"repository"`
	}
	started := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	d := document{Envelope: NewEnvelope("pr wait", started), Repository: "giantswarm/devctl"}
	d.Finish(started.Add(time.Second), VerdictGreen, nil)

	var buf bytes.Buffer
	if err := Emit(&buf, d); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("}\n")) {
		t.Fatalf("no trailing newline: %q", buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"command", "schemaVersion", "exitCode", "verdict", "reason", "warnings", "startedAt", "finishedAt", "repository"} {
		if _, ok := got[key]; !ok {
			t.Errorf("document lacks %q: %v", key, got)
		}
	}
	if got["warnings"] == nil {
		t.Errorf("warnings must be an empty array, not null")
	}
}

func TestSystemClockScale(t *testing.T) {
	t.Setenv(EnvTimeScale, "0.001")
	c, err := SystemClock()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Scaled(5 * time.Minute); got != 300*time.Millisecond {
		t.Fatalf("Scaled(5m) = %s", got)
	}
	if got := c.Scaled(time.Microsecond); got != time.Millisecond {
		t.Fatalf("floor: %s", got)
	}
	if got := c.Scaled(0); got != 0 {
		t.Fatalf("Scaled(0) = %s", got)
	}

	t.Setenv(EnvTimeScale, "")
	c, err = SystemClock()
	if err != nil || c.Scale() != 1 {
		t.Fatalf("unset: %v %v", c.Scale(), err)
	}

	for _, bad := range []string{"0", "-1", "fast"} {
		t.Setenv(EnvTimeScale, bad)
		if _, err := SystemClock(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestClockSleepHonoursContext(t *testing.T) {
	c := NewClock(1, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep = %v", err)
	}
	if err := NewClock(0.001, nil).Sleep(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := NewClock(1, func() time.Time { return fixed }).Now(); got != fixed {
		t.Fatalf("Now = %s", got)
	}
}

func TestEndpointsFromEnv(t *testing.T) {
	for _, key := range []string{EnvGitHubAPIURL, EnvGitHubOAuthURL, EnvCircleCIAPIURL, EnvCircleCIOAuthURL, EnvRegistryPublic, EnvRegistryPrivate, EnvRegistryInsecure, EnvKeyringFile} {
		t.Setenv(key, "")
	}
	if got := EndpointsFromEnv(); got != DefaultEndpoints() {
		t.Fatalf("defaults: %+v", got)
	}
	t.Setenv(EnvGitHubAPIURL, "http://127.0.0.1:1/api/")
	t.Setenv(EnvCircleCIOAuthURL, "http://127.0.0.1:2")
	t.Setenv(EnvRegistryInsecure, "1")
	t.Setenv(EnvKeyringFile, "/tmp/keyring.json")
	got := EndpointsFromEnv()
	if got.GitHubAPIURL != "http://127.0.0.1:1/api" || got.CircleCIOAuthURL != "http://127.0.0.1:2" || !got.RegistryInsecure || got.KeyringFile != "/tmp/keyring.json" {
		t.Fatalf("overrides: %+v", got)
	}
	if got.GitHubOAuthURL != DefaultEndpoints().GitHubOAuthURL {
		t.Fatalf("untouched default changed: %+v", got)
	}
}

func TestProgress(t *testing.T) {
	var buf bytes.Buffer
	NewProgress(&buf, false).Printf("hidden %d", 1)
	if buf.Len() != 0 {
		t.Fatalf("disabled progress wrote %q", buf.String())
	}
	NewProgress(&buf, true).Printf("step %d", 2)
	if buf.String() != "step 2\n" {
		t.Fatalf("got %q", buf.String())
	}
	var nilProgress *Progress
	nilProgress.Printf("no panic")
}
