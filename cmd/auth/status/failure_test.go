package status

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
)

// Tooling failures must stay distinguishable from a missing login, including
// failures before the status store opens and after an identity was read.
func TestStatusFailureDocuments(t *testing.T) {
	for _, stage := range []string{"version gate", "auth setup", authstore.UserGitHub, authstore.UserCircleCI, authstore.UserMuster} {
		t.Run(stage, func(t *testing.T) {
			r, stdout, stderr := newRunner(t, nil)
			failure := errors.New("synthetic " + stage + " failure")
			switch stage {
			case "version gate":
				r.gate = func(bool) error { return failure }
				r.open = func(io.Writer) (*authstore.Auth, error) {
					t.Fatal("a refused version gate must stop before opening the status store")
					return nil, failure
				}
			case "auth setup":
				r.open = func(io.Writer) (*authstore.Auth, error) { return nil, failure }
			default:
				r.open = func(w io.Writer) (*authstore.Auth, error) {
					return authstore.New(authstore.Config{
						Store:  &failedReadStore{t: t, user: stage, err: failure},
						Clock:  agentcli.NewClock(1, func() time.Time { return now }),
						Stderr: w,
					})
				}
			}
			assertFailureDocument(t, stdout, stderr, r.run(nil), failure.Error())
		})
	}
}

// Exercise the real constructor, auth setup, and file-store error handling:
// runner-only fakes cannot catch a dropped dependency or output binding.
func TestStatusCommandSetupAndReadFailures(t *testing.T) {
	for _, scenario := range []string{"invalid clock", "unreadable store", "corrupt store"} {
		t.Run(scenario, func(t *testing.T) {
			for _, name := range append([]string{authstore.EnvCI}, authstore.GitHubEnvVars...) {
				t.Setenv(name, "")
			}
			// Keep the version lookup out of these auth setup/read scenarios.
			t.Setenv("DEVCTL_UNSAFE_FORCE_VERSION", project.Version())
			t.Setenv(agentcli.EnvTimeScale, "1")
			for _, name := range []string{agentcli.EnvGitHubAPIURL, agentcli.EnvGitHubOAuthURL, agentcli.EnvCircleCIAPIURL, agentcli.EnvCircleCIOAuthURL, agentcli.EnvMusterURL} {
				t.Setenv(name, "http://127.0.0.1:1")
			}
			path := filepath.Join(t.TempDir(), "keyring.json")
			var wantReason string
			switch scenario {
			case "invalid clock":
				t.Setenv(agentcli.EnvTimeScale, "invalid-test-scale")
				wantReason = agentcli.EnvTimeScale
			case "unreadable store":
				path = t.TempDir() // A directory cannot be read as a keyring file.
				wantReason = "reading the keyring file " + path
			case "corrupt store":
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
				wantReason = "the keyring file " + path + " is not devctl's"
			}
			t.Setenv(agentcli.EnvKeyringFile, path)

			var stdout, stderr bytes.Buffer
			cmd, err := New(Config{Stdout: &stdout, Stderr: &stderr})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			// The real root also suppresses Cobra's automatic error/usage text.
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{})
			assertFailureDocument(t, &stdout, &stderr, cmd.Execute(), wantReason)
		})
	}
}

func assertFailureDocument(t *testing.T, stdout, stderr *bytes.Buffer, err error, wantReason string) {
	t.Helper()
	var exitErr *agentcli.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitUsage || exitErr.Verdict != agentcli.VerdictUsage {
		t.Fatalf("error = %#v, want a usage ExitError", err)
	}
	var doc document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not one complete JSON document: %v\n%s", err, stdout.String())
	}
	if doc.Command != command || doc.SchemaVersion != agentcli.SchemaVersion || doc.ExitCode != exitErr.Code || doc.Verdict != exitErr.Verdict || doc.Reason != exitErr.Reason || !strings.Contains(doc.Reason, wantReason) {
		t.Fatalf("failure envelope = %+v, want reason containing %q and the returned outcome", doc.Envelope, wantReason)
	}
	if doc.StartedAt.IsZero() || doc.FinishedAt.Before(doc.StartedAt) {
		t.Fatalf("incomplete timestamps: %+v", doc.Envelope)
	}
	if doc.Warnings == nil {
		t.Fatal("envelope warnings must be an array")
	}
	for name, id := range map[string]authstore.Identity{"github": doc.GitHub, "circleci": doc.CircleCI, "muster": doc.Muster} {
		if id.Warnings == nil {
			t.Errorf("%s warnings must be an array", name)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr without --progress = %q, want silence", stderr.String())
	}
}

type failedReadStore struct {
	t    *testing.T
	user string
	err  error
}

func (s *failedReadStore) Get(user string) (authstore.Record, error) {
	if user == s.user {
		return authstore.Record{}, s.err
	}
	return authstore.Record{Login: "fixture-user", Token: "synthetic-access-token", ExpiresAt: now.Add(time.Hour)}, nil
}

func (s *failedReadStore) Set(string, authstore.Record) error {
	s.t.Fatal("status must not write credentials")
	return nil
}

func (s *failedReadStore) Delete(string) error {
	s.t.Fatal("status must not delete credentials")
	return nil
}

func (s *failedReadStore) Lock(string) (func(), error) {
	s.t.Fatal("status must not lock credentials for a refresh")
	return nil, nil
}
