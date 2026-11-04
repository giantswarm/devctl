package reap_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

func TestReapPreservesUntrackedComponentFiles(t *testing.T) {
	for _, mode := range []string{"normal", "dry-run", "quoted-reference", "ignored"} {
		t.Run(mode, func(t *testing.T) {
			dir, origin := newReapableFixture(t, "graveler", "hello-world", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
			collections := filepath.Join(dir, "management-clusters", "graveler", "collections")
			component := filepath.Join(collections, "reservations", "hello-world")
			if mode == "quoted-reference" {
				kustomization := filepath.Join(collections, "kustomization.yaml")
				data, err := os.ReadFile(kustomization)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.ReplaceAll(data, []byte("- reservations/hello-world"), []byte("- \"reservations/hello-world\""))
				if err := os.WriteFile(kustomization, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "ignored" {
				if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("notes.txt\nnested/\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			gittest.RunGit(t, dir, "add", "-A")
			// These two variants change tracked fixture input before adding notes.
			if mode == "quoted-reference" || mode == "ignored" {
				gittest.RunGit(t, dir, "commit", "-m", "prepare component fixture")
				gittest.RunGit(t, dir, "push", "origin", "main")
			}
			before := gittest.GitOutput(t, origin, "rev-parse", "main")
			if err := os.MkdirAll(filepath.Join(component, "nested"), 0o750); err != nil {
				t.Fatal(err)
			}
			notes := map[string]string{"notes.txt": "keep my notes\n", "nested/private.txt": "keep nested bytes\n"}
			for name, data := range notes {
				if err := os.WriteFile(filepath.Join(component, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			cmd := newCommandWithStderr(t, &stdout, &stderr)
			args := []string{flagRepoDir, dir, flagUser, "reservation-reaper"}
			if mode == "dry-run" {
				args = append(args, "--dry-run")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "untracked files") {
				t.Fatalf("got %v, want refusal before deleting user files", err)
			}
			for name, want := range notes {
				got, err := os.ReadFile(filepath.Join(component, name))
				if err != nil || string(got) != want {
					t.Errorf("%s: got %q, error %v; want %q", name, got, err, want)
				}
			}
			if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != before {
				t.Errorf("local HEAD changed: %s, want %s", got, before)
			}
			if got := gittest.GitOutput(t, origin, "rev-parse", "main"); got != before {
				t.Errorf("origin changed: %s, want %s", got, before)
			}
			if diff := gittest.GitOutput(t, dir, "diff", "HEAD"); diff != "" {
				t.Errorf("refusal changed tracked files: %s", diff)
			}
			if stdout.Len() != 0 {
				t.Errorf("refusal reported a release: %q", stdout.String())
			}
		})
	}
}
