package rolloutwait

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// fakeTsh is a tsh that answers status with $FAKE_TSH_STATUS and exit
// $FAKE_TSH_STATUS_EXIT, and for kube login records its KUBECONFIG and
// arguments in $FAKE_TSH_LOG and writes teleportKubeconfig there.
const fakeTsh = `#!/bin/sh
case "$1" in
status)
  printf '%s' "$FAKE_TSH_STATUS"
  [ -n "$FAKE_TSH_STATUS_ERR" ] && printf '\033[31mERROR: \033[0m%s\n' "$FAKE_TSH_STATUS_ERR" >&2
  exit "${FAKE_TSH_STATUS_EXIT:-0}" ;;
kube)
  printf '%s %s' "$KUBECONFIG" "$*" > "$FAKE_TSH_LOG"
  cp "$FAKE_TSH_KUBECONFIG" "$KUBECONFIG" ;;
esac
`

// installFakeTsh writes fakeTsh and returns a Tsh running it at now.
func installFakeTsh(t *testing.T, now time.Time) Tsh {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake tsh is a shell script")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "tsh")
	if err := os.WriteFile(binary, []byte(fakeTsh), 0o700); err != nil { // #nosec G306 -- the fake must be executable
		t.Fatal(err)
	}
	t.Setenv("FAKE_TSH_LOG", filepath.Join(dir, "log"))
	t.Setenv("FAKE_TSH_KUBECONFIG", writeFile(t, "teleport", teleportKubeconfig))
	return Tsh{Binary: binary, Now: func() time.Time { return now }}
}

func TestTshLoggedIn(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		status   string
		exit     string
		stderr   string
		loggedIn bool
		why      string
	}{
		{name: "valid profile", status: `{"active":{"profile_url":"https://teleport.giantswarm.io:443","valid_until":"2026-10-15T11:05:16+02:00"}}`, loggedIn: true},
		{name: "expired profile", status: `{"active":{"profile_url":"https://teleport.giantswarm.io:443","valid_until":"2026-10-15T10:59:00+02:00"}}`, why: "expired at 2026-10-15T10:59:00+02:00"},
		{name: "no active profile", status: `{}`, why: "no active tsh profile"},
		{name: "not logged in", exit: "1", stderr: "Not logged in.", why: "Not logged in."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tsh := installFakeTsh(t, now)
			t.Setenv("FAKE_TSH_STATUS", tc.status)
			t.Setenv("FAKE_TSH_STATUS_EXIT", tc.exit)
			t.Setenv("FAKE_TSH_STATUS_ERR", tc.stderr)
			loggedIn, why, err := tsh.LoggedIn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if loggedIn != tc.loggedIn || !strings.Contains(why, tc.why) || strings.Contains(why, "\x1b") {
				t.Errorf("want %t %q, got %t %q", tc.loggedIn, tc.why, loggedIn, why)
			}
		})
	}
}

func TestTshNotInstalled(t *testing.T) {
	tsh := Tsh{Binary: filepath.Join(t.TempDir(), "tsh")}
	if _, _, err := tsh.LoggedIn(context.Background()); !errors.Is(err, ErrNoTsh) {
		t.Errorf("LoggedIn: want ErrNoTsh, got %v", err)
	}
	if err := tsh.KubeLogin(context.Background(), "myinstallation", teleportContext, "config"); !errors.Is(err, ErrNoTsh) {
		t.Errorf("KubeLogin: want ErrNoTsh, got %v", err)
	}
}

// TestOpenClusterThroughTsh runs the whole fallback through the tsh binary:
// the login sees only the private kubeconfig, the machine's stays as it was.
func TestOpenClusterThroughTsh(t *testing.T) {
	tsh := installFakeTsh(t, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	t.Setenv("FAKE_TSH_STATUS", `{"active":{"profile_url":"https://teleport.giantswarm.io:443","valid_until":"2026-10-15T11:05:16+02:00"}}`)
	machine := machineKubeconfig(t)

	client, err := OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: teleportContext, Teleport: tsh})
	if err != nil || client == nil {
		t.Fatalf("want the context opened after the login, got %v", err)
	}
	log, err := os.ReadFile(os.Getenv("FAKE_TSH_LOG")) // #nosec G304 G703 -- the log of the test's own fake tsh
	if err != nil {
		t.Fatalf("tsh kube login did not run: %v", err)
	}
	private, args, _ := strings.Cut(string(log), " ")
	if private == "" || private == machine {
		t.Errorf("tsh kube login ran with KUBECONFIG %q, want a private file", private)
	}
	if args != "kube login myinstallation --set-context-name="+teleportContext {
		t.Errorf("tsh arguments: %q", args)
	}
	if _, err := os.Stat(private); !errors.Is(err, os.ErrNotExist) { // #nosec G703 -- the path the test's own fake tsh recorded
		t.Errorf("the private kubeconfig is removed on return: %v", err)
	}
	if content, _ := os.ReadFile(machine); string(content) != kubeconfig { // #nosec G304 -- a file of the test's own temporary directory
		t.Errorf("the machine's kubeconfig is never written:\n%s", content)
	}
}

func TestOpenClusterThroughTshNotLoggedInIsExit8(t *testing.T) {
	tsh := installFakeTsh(t, time.Now())
	t.Setenv("FAKE_TSH_STATUS_EXIT", "1")
	t.Setenv("FAKE_TSH_STATUS_ERR", "Not logged in.")
	machineKubeconfig(t)

	_, err := OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: teleportContext, Teleport: tsh})
	if agentcli.Exit(err) != agentcli.ExitAuthRequired {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitAuthRequired, agentcli.Exit(err), err)
	}
	if _, err := os.Stat(os.Getenv("FAKE_TSH_LOG")); !errors.Is(err, os.ErrNotExist) { // #nosec G703 -- the log of the test's own fake tsh
		t.Errorf("tsh kube login must not run without a login: %v", err)
	}
}
