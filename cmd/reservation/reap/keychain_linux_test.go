//go:build linux

package reap_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestReapReleasesExpiredReservationsWithoutASessionBus(t *testing.T) {
	until := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	dir, origin := newReapableFixture(t, "graveler", "hello-world", until)
	before := gittest.GitOutput(t, origin, "rev-parse", "main")

	var stdout, stderr bytes.Buffer
	cmd := newCommandWithStderr(t, &stdout, &stderr)
	// Exercise the non-CI fallback without consulting a real keychain or a
	// stored file. The bus address names a socket that does not exist.
	t.Setenv("CI", "")
	t.Setenv(agentcli.EnvKeyringFile, "")
	t.Setenv(agentcli.EnvTimeScale, "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-session-bus"))
	cmd.SetArgs([]string{flagRepoDir, dir, flagUser, "reservation-reaper"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("reap with an optional, unreachable keychain: %v", err)
	}
	if !strings.Contains(stdout.String(), "\texpired\t") {
		t.Errorf("stdout does not report the expired reservation: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no GitHub token found") {
		t.Errorf("stderr does not explain the skipped PR check: %q", stderr.String())
	}
	if got := gittest.GitOutput(t, origin, "rev-parse", "main"); got == before {
		t.Error("the expired release was not pushed")
	}
	if _, err := os.Stat(filepath.Join(dir, "management-clusters", "graveler", "collections", "reservations", "hello-world")); !os.IsNotExist(err) {
		t.Errorf("reservation component still exists: %v", err)
	}
}
