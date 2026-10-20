//go:build !windows

package authexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// TestMain makes the test binary a devctl started as gh when a gh link to it
// runs it, with a fixed token instead of a login.
func TestMain(m *testing.M) {
	if Proxy(os.Args[0]) != "" {
		c := Default()
		c.Token = func(context.Context, time.Duration) (authstore.Token, error) {
			return authstore.Token{Value: "app-token"}, nil
		}
		c.Remotes = func() string { return "" }
		os.Exit(Run(context.Background(), c, "gh", os.Args[1:]))
	}
	os.Exit(m.Run())
}

// forwardingShim is a gh shim that passes every call to the first gh on PATH
// outside its own directory, as a test harness's or sandbox's shim does.
const forwardingShim = `#!/bin/sh
own=${0%/*}
IFS=:
for d in $PATH; do
	if [ "$d" != "$own" ] && [ -x "$d/gh" ]; then exec "$d/gh" "$@"; fi
done
exit 127
`

// linkFixture is a gh link to the test binary in agent-bin, the passthrough
// shim in shim and a real gh in bin that prints its arguments.
func linkFixture(t *testing.T) (agentBin, shimBin, realBin string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	agentBin, shimBin, realBin = filepath.Join(dir, "agent-bin"), filepath.Join(dir, "shim"), filepath.Join(dir, "bin")
	for _, d := range []string{agentBin, shimBin, realBin} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(self, filepath.Join(agentBin, "gh")); err != nil {
		t.Fatal(err)
	}
	for path, script := range map[string]string{
		filepath.Join(shimBin, "gh"): forwardingShim,
		filepath.Join(realBin, "gh"): "#!/bin/sh\necho \"real gh $*\"\n",
	} {
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // nolint:gosec // an executable fixture
			t.Fatal(err)
		}
	}
	return agentBin, shimBin, realBin
}

// runGH runs gh --version on path, bounded so a loop fails the test instead
// of hanging it.
func runGH(t *testing.T, path string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "--version")
	cmd.Path = filepath.Join(strings.Split(path, string(os.PathListSeparator))[0], "gh")
	cmd.Env = []string{"PATH=" + path, "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("gh --version did not return: %s", out)
	}
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestLinkPastShimAhead(t *testing.T) {
	agentBin, shimBin, realBin := linkFixture(t)
	out, code := runGH(t, strings.Join([]string{shimBin, agentBin, realBin}, string(os.PathListSeparator)))
	if code != 0 || out != "real gh --version\n" {
		t.Fatalf("gh --version through a shim ahead of the link = %d %q, want the real gh once", code, out)
	}
}

func TestLinkRefusesLoopBack(t *testing.T) {
	agentBin, shimBin, realBin := linkFixture(t)
	// The shim past the link hands the call back to the link, first on PATH.
	out, code := runGH(t, strings.Join([]string{agentBin, shimBin, realBin}, string(os.PathListSeparator)))
	shim := filepath.Join(shimBin, "gh")
	if code != agentcli.ExitUsage || !strings.Contains(out, "refusing to run "+shim) ||
		strings.Count(out, shim) != MaxChain+1 || strings.Contains(out, "real gh") {
		t.Fatalf("gh --version looping back to the link = %d %q, want a refusal naming the shim", code, out)
	}
}
