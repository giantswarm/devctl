package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// offlineContainer is a plain container: no session bus, so no keychain, no
// token in the environment, an empty home, and a GitHub that counts every
// request it gets.
func offlineContainer(t *testing.T) *atomic.Int32 {
	t.Helper()

	var hits atomic.Int32
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(github.Close)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(home, "no-bus"))
	t.Setenv(authstore.EnvCI, "")
	for _, name := range authstore.GitHubEnvVars {
		t.Setenv(name, "")
	}
	t.Setenv(agentcli.EnvKeyringFile, "")
	t.Setenv(agentcli.EnvGitHubAPIURL, github.URL)
	t.Setenv("DEVCTL_UNSAFE_FORCE_VERSION", "")

	t.Chdir(t.TempDir())

	return &hits
}

func execute(t *testing.T, args ...string) (int, string) {
	t.Helper()

	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root, err := New(Config{Logger: logger, Stderr: &out, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)

	return Execute(root, &out), out.String()
}

// TestGenCircleCIOffline runs `gen circleci` the way the devctl image runs it
// in a plain container: it exits 0 and writes the pipeline without opening
// the keychain or asking GitHub.
func TestGenCircleCIOffline(t *testing.T) {
	hits := offlineContainer(t)

	code, out := execute(t, "gen", "circleci", "--repo-name", "example", "--flavour", "cli", "--language", "go")
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	if strings.Contains(out, "keychain") {
		t.Errorf("output names the keychain:\n%s", out)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("GitHub was asked %d times, want 0", n)
	}
	if _, err := os.Stat(filepath.Join(".circleci", "config.yml")); err != nil {
		t.Errorf("no pipeline written: %v", err)
	}
}

// TestGateOutsideGen is the control of [TestGenCircleCIOffline]: a command
// outside `gen` in the same container still runs the version gate, which
// cannot pass there (a test binary is version "dev", and neither the keychain
// nor GitHub answers).
func TestGateOutsideGen(t *testing.T) {
	offlineContainer(t)

	code, out := execute(t)
	if code == 0 {
		t.Fatalf("exit 0, want the gate's error:\n%s", out)
	}
}
