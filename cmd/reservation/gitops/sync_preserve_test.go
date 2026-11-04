package gitops

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

func TestSyncPreservesUntrackedFilesInTheFastForward(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		name := "untracked"
		if ignored {
			name = "ignored"
		}
		t.Run(name, func(t *testing.T) {
			dir, other := newPair(t)
			before := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
			if ignored {
				if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("notes\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			const local = "local notes that must survive\n"
			file := filepath.Join(dir, "notes")
			if err := os.WriteFile(file, []byte(local), 0o600); err != nil {
				t.Fatal(err)
			}
			commit(t, other, "notes")
			gittest.RunGit(t, other, "push", "origin", "main")

			if err := Sync(context.Background(), dir, &bytes.Buffer{}); !IsSync(err) {
				t.Fatalf("got %v, want a refusal to overwrite the local file", err)
			}
			if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != before {
				t.Errorf("HEAD moved from %s to %s", before, got)
			}
			got, err := os.ReadFile(file)
			if err != nil || string(got) != local {
				t.Errorf("local file: got %q, %v; want %q", got, err, local)
			}
		})
	}
}
