package reap_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestReapReportsOutputFailures(t *testing.T) {
	for _, format := range []string{"table", "json"} {
		for _, brokenCluster := range []bool{false, true} {
			name := format + "/successful-sweep"
			if brokenCluster {
				name = format + "/partial-sweep"
			}
			t.Run(name, func(t *testing.T) {
				until := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
				dir, origin := newReapableFixture(t, "graveler", "hello-world", until)
				if brokenCluster {
					addBrokenCluster(t, dir)
				}
				before := gittest.GitOutput(t, origin, "rev-parse", "main")

				writeErr := errors.New("output destination failed")
				var stderr bytes.Buffer
				cmd := newCommandWithStderr(t, failingWriter{err: writeErr}, &stderr)
				cmd.SetArgs([]string{flagRepoDir, dir, flagUser, "reservation-reaper", "--output", format})

				err := cmd.Execute()
				if !errors.Is(err, writeErr) {
					t.Fatalf("got %v, want the output failure", err)
				}
				if brokenCluster && !strings.Contains(err.Error(), "broken-cluster") {
					t.Errorf("output failure hid the cluster failure: %v", err)
				}
				if !strings.Contains(stderr.String(), "Flux applies the change") {
					t.Errorf("stderr does not report the change that landed: %q", stderr.String())
				}
				pushed := gittest.GitOutput(t, origin, "rev-parse", "main")
				if pushed == before {
					t.Fatal("fixture's expired reservation was not released")
				}
				if head := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); head != pushed {
					t.Errorf("local HEAD %s differs from origin main %s", head, pushed)
				}
			})
		}
	}
}

func addBrokenCluster(t *testing.T, dir string) {
	t.Helper()

	clusterDir := filepath.Join(dir, "management-clusters", "broken-cluster")
	if err := os.MkdirAll(clusterDir, 0o750); err != nil {
		t.Fatal(err)
	}
	configMap := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: reservations\n  namespace: giantswarm\ndata:\n" +
		"  broken-app: '{user: alice, branch: fix/crash, scope: app, from: not-a-date, until: not-a-date}'\n"
	if err := os.WriteFile(filepath.Join(clusterDir, "configmap-reservations.yaml"), []byte(configMap), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.RunGit(t, dir, "add", "management-clusters/broken-cluster")
	gittest.RunGit(t, dir, "commit", "-m", "add broken cluster")
	gittest.RunGit(t, dir, "push", "origin", "main")
}
