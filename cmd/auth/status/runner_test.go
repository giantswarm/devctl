package status

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

var now = time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

func newRunner(t *testing.T, records map[string]authstore.Record) (*runner, *bytes.Buffer, *bytes.Buffer) {
	store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	for user, record := range records {
		if err := store.Set(user, record); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	r := &runner{
		flag:   &flag{},
		stdout: &stdout,
		stderr: &stderr,
		open: func(w io.Writer) (*authstore.Auth, error) {
			return authstore.New(authstore.Config{
				Store:  store,
				Clock:  agentcli.NewClock(1, func() time.Time { return now }),
				Stderr: w,
			})
		},
	}
	return r, &stdout, &stderr
}

func TestStatusWithoutTokensIsExit8(t *testing.T) {
	r, stdout, _ := newRunner(t, nil)
	err := r.run(nil)
	var exitErr *agentcli.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitAuthRequired {
		t.Fatalf("err = %#v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout.String())
	}
	if doc["command"] != "auth status" || doc["exitCode"] != float64(8) || doc["verdict"] != "auth_required" {
		t.Fatalf("envelope = %v", doc)
	}
	if reason, _ := doc["reason"].(string); !strings.Contains(reason, "GitHub authentication required") || !strings.Contains(reason, "devctl auth login") {
		t.Fatalf("reason = %q", doc["reason"])
	}
	github := doc["github"].(map[string]any)
	if github["present"] != false {
		t.Fatalf("github = %v", github)
	}
}

func TestStatusWithTokensIsGreenAndSilent(t *testing.T) {
	r, stdout, stderr := newRunner(t, map[string]authstore.Record{
		authstore.UserGitHub: {Login: "octocat", Token: "ghu_secret", ExpiresAt: now.Add(-time.Hour),
			RefreshToken: "ghr_secret", RefreshExpiresAt: now.Add(100 * 24 * time.Hour)},
		authstore.UserCircleCI: {Login: "octocat", Token: "ccipat_secret", ExpiresAt: now.Add(5 * 24 * time.Hour), ClientID: "client-1"},
	})
	if err := r.run(nil); err != nil {
		t.Fatalf("err = %v", err)
	}
	out := stdout.String()
	for _, secret := range []string{"ghu_secret", "ghr_secret", "ccipat_secret"} {
		if strings.Contains(out, secret) {
			t.Fatalf("stdout carries %s:\n%s", secret, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr without --progress: %q", stderr.String())
	}
	var doc struct {
		agentcli.Envelope
		authstore.Status
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ExitCode != 0 || doc.Verdict != agentcli.VerdictGreen || doc.SchemaVersion != 1 {
		t.Fatalf("envelope = %+v", doc.Envelope)
	}
	if !doc.GitHub.Expired || !doc.GitHub.Refreshable || doc.GitHub.Login != "octocat" {
		t.Fatalf("github = %+v", doc.GitHub)
	}
	if len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "5 day(s)") || len(doc.CircleCI.Warnings) != 1 {
		t.Fatalf("warnings = %v / %v", doc.Warnings, doc.CircleCI.Warnings)
	}

	r.flag.Progress = true
	stdout.Reset()
	if err := r.run(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "reading the keychain") {
		t.Fatalf("--progress wrote nothing: %q", stderr.String())
	}
}
