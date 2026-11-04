package reservation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestReservePreservesUntrackedComponentFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail bool
	}{
		{"case 0: successful reserve", false},
		{"case 1: rollback after component writes", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})
			if tc.fail {
				writeLocalFixtureFile(t, dir, pathReservations, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: reservations\n  namespace: giantswarm\n")
				gittest.RunGit(t, dir, "commit", "-qam", "remove data key")
			}
			notes := filepath.ToSlash(filepath.Join(filepath.Dir(pathComponent), "notes.txt"))
			writeLocalFixtureFile(t, dir, notes, "local work\n")
			before := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
			_, err := reservation.Reserve(testRequest(dir))
			if tc.fail && !reservation.IsInvalidConfig(err) {
				t.Fatalf("want invalid config, got %v", err)
			}
			if !tc.fail && err != nil {
				t.Fatal(err)
			}
			if got := readFixtureFile(t, dir, notes); got != "local work\n" {
				t.Fatalf("user file changed: %q", got)
			}
			if got := gittest.GitOutput(t, dir, "ls-files", "--", notes); got != "" {
				t.Fatalf("user file committed: %s", got)
			}
			if tc.fail {
				if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != before {
					t.Fatal("failed reserve moved HEAD")
				}
				if got := gittest.GitOutput(t, dir, "diff", "HEAD"); got != "" {
					t.Fatalf("tracked files changed: %s", got)
				}
				for _, name := range []string{pathSource, pathComponent} {
					if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
						t.Fatalf("generated file %s survived rollback: %v", name, err)
					}
				}
			}
		})
	}
}

func TestReserveRefusesUntrackedOutputFiles(t *testing.T) {
	for _, name := range []string{pathSource, pathComponent} {
		t.Run(name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})
			writeLocalFixtureFile(t, dir, name, "local output\n")
			before := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
			status := gittest.GitOutput(t, dir, "status", "--porcelain", "--untracked-files=all")
			_, err := reservation.Reserve(testRequest(dir))
			if !reservation.IsInvalidConfig(err) || !strings.Contains(err.Error(), name) {
				t.Fatalf("want refusal naming output, got %v", err)
			}
			if got := readFixtureFile(t, dir, name); got != "local output\n" {
				t.Fatalf("output changed: %q", got)
			}
			if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != before {
				t.Fatal("refusal moved HEAD")
			}
			if got := gittest.GitOutput(t, dir, "status", "--porcelain", "--untracked-files=all"); got != status {
				t.Fatalf("refusal changed checkout: %s", got)
			}
		})
	}
}

func writeLocalFixtureFile(t *testing.T, dir, name, content string) {
	t.Helper()
	target := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { //nolint:gosec // a temporary test fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil { //nolint:gosec // a temporary test fixture
		t.Fatal(err)
	}
}
