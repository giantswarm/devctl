package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

func TestResetCommandWorksWithShellSpecialCharactersInTheCheckoutPath(t *testing.T) {
	for _, name := range []string{"checkout with spaces", "checkout's notes", "checkout; false"} {
		t.Run(name, func(t *testing.T) {
			_, other := newPair(t)
			origin := gittest.GitOutput(t, other, "remote", "get-url", "origin")
			parent := t.TempDir()
			dir := filepath.Join(parent, name)
			if err := os.Mkdir(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			gittest.RunGit(t, dir, "clone", origin, ".")
			before := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
			commit(t, dir, "pending")

			// Exercise the exact command an engineer copies from the diagnostic.
			out, err := exec.Command("sh", "-c", resetCommand(dir)).CombinedOutput() //nolint:gosec // generated recovery command against a test fixture
			if err != nil {
				t.Fatalf("recovery command: %v\n%s", err, out)
			}
			if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != before {
				t.Errorf("recovery left HEAD at %s, want %s", got, before)
			}
		})
	}
}
