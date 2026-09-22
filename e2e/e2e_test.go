// Package e2e runs the built devctl against in-process mocks of GitHub,
// CircleCI and the registry, one scenario per directory under scenarios/.
// e2e/README.md documents the format; this file is the runner.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
	"github.com/giantswarm/devctl/v8/e2e/mock/registry"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/e2e/scenario"
)

// version is the version the binary is built with. The update check on every
// command is skipped when DEVCTL_UNSAFE_FORCE_VERSION names the running
// version, which is how the harness keeps the binary off the network.
const version = "0.0.0-e2e"

// TimeScale multiplies every sleep and timeout of the binary: a thirty-minute
// wait takes 1.8 seconds.
const TimeScale = "0.001"

// binary is the devctl TestMain builds.
var binary string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "devctl-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	binary = filepath.Join(dir, "devctl")
	ldflags := fmt.Sprintf("-X github.com/giantswarm/devctl/v8/pkg/project.version=%s -X github.com/giantswarm/devctl/v8/pkg/project.gitSHA=e2e", version)
	build := exec.Command("go", "build", "-o", binary, "-ldflags", ldflags, ".") //nolint:gosec // the arguments are constants of this file
	build.Dir = ".."
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: building devctl:", err)
		return 1
	}
	return m.Run()
}

// declareSourceInputs stats every file of the module. go test caches a
// package's result keyed on the files a test opened or stat'ed inside the
// module (recorded while m.Run is active, hence not from TestMain); the
// binary is built from the whole module, so every file of it is an input,
// and a change anywhere reruns the scenarios.
func declareSourceInputs(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".build", "e2e":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		_, err = os.Stat(path)
		return err
	})
}

func TestScenarios(t *testing.T) {
	if err := declareSourceInputs(".."); err != nil {
		t.Fatal(err)
	}
	dirs, err := scenario.Discover("scenarios")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no scenario under scenarios/")
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			runScenario(t, dir)
		})
	}
}

// mocks are one scenario's servers.
type mocks struct {
	github          *github.Server
	circleci        *circleci.Server
	registry        *registry.Server
	privateRegistry *registry.Server
	muster          *muster.Server
}

func startMocks(t *testing.T, sc *scenario.Scenario) *mocks {
	t.Helper()
	m := &mocks{}
	var err error
	if m.github, err = github.Start(sc.GitHub.Routes); err != nil {
		t.Fatalf("github mock: %v", err)
	}
	t.Cleanup(m.github.Close)
	if m.circleci, err = circleci.Start(sc.CircleCI.Routes); err != nil {
		t.Fatalf("circleci mock: %v", err)
	}
	t.Cleanup(m.circleci.Close)
	if m.registry, err = registry.Start(sc.Registry.Routes, sc.Registry.StaleLogin); err != nil {
		t.Fatalf("registry mock: %v", err)
	}
	t.Cleanup(m.registry.Close)
	if m.privateRegistry, err = registry.Start(sc.PrivateRegistry.Routes, sc.PrivateRegistry.StaleLogin); err != nil {
		t.Fatalf("private registry mock: %v", err)
	}
	t.Cleanup(m.privateRegistry.Close)
	m.muster = muster.Start(sc.Muster)
	t.Cleanup(m.muster.Close)
	return m
}

// environment is the binary's whole environment: the seams pointed at the
// mocks, a home of its own, an empty PATH, and the scenario's variables last
// so they win.
func environment(t *testing.T, sc *scenario.Scenario, m *mocks) []string {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "bin")
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatal(err)
	}
	keyring := filepath.Join(home, "keyring.json")
	if sc.Keyring != nil {
		data, err := json.Marshal(sc.Keyring)
		if err != nil {
			t.Fatalf("keyring: %v", err)
		}
		if err := os.WriteFile(keyring, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"TMPDIR=" + home,
		"PATH=" + path,
		"DEVCTL_UNSAFE_FORCE_VERSION=" + version,
		"DEVCTL_TIME_SCALE=" + TimeScale,
		"DEVCTL_GITHUB_API_URL=" + m.github.URL,
		"DEVCTL_GITHUB_OAUTH_URL=" + m.github.URL,
		"DEVCTL_CIRCLECI_API_URL=" + m.circleci.APIURL(),
		"DEVCTL_CIRCLECI_OAUTH_URL=" + m.circleci.URL,
		"DEVCTL_REGISTRY_PUBLIC=" + m.registry.Host(),
		"DEVCTL_REGISTRY_PRIVATE=" + m.privateRegistry.Host(),
		"DEVCTL_REGISTRY_INSECURE=1",
		"DEVCTL_MUSTER_URL=" + m.muster.MCPURL(),
		"DEVCTL_KEYRING_FILE=" + keyring,
	}
	for key, value := range sc.Env {
		env = append(env, key+"="+value)
	}
	return env
}

func runScenario(t *testing.T, dir string) {
	sc, expected, err := scenario.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := startMocks(t, sc)

	ctx, cancel := context.WithTimeout(context.Background(), sc.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, sc.Args...) //nolint:gosec // the binary is the one TestMain built, the arguments are the scenario's
	cmd.Dir = dir
	cmd.Env = environment(t, sc, m)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if sc.Browser {
		person := &human{}
		cmd.Stderr = io.MultiWriter(&stderr, person)
		defer person.wait()
	}

	err = cmd.Run()
	exitCode := 0
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case ctx.Err() != nil:
		t.Errorf("devctl did not finish within %s", sc.Timeout)
	case errors.As(err, &exitErr):
		exitCode = exitErr.ExitCode()
	default:
		t.Errorf("running devctl: %v", err)
	}
	if exitCode != expected.ExitCode {
		t.Errorf("exit code: want %d, got %d", expected.ExitCode, exitCode)
	}
	if expected.ComparesJSON() {
		compareJSON(t, expected.JSON, stdout.Bytes())
	}
	if t.Failed() {
		t.Logf("scenario: %s\nargs: %s\nstdout:\n%s\nstderr:\n%s\nrequests:\n%s",
			sc.Description, strings.Join(sc.Args, " "), stdout.String(), stderr.String(), m.requests())
	}
}

func compareJSON(t *testing.T, expected, stdout []byte) {
	t.Helper()
	var want, got any
	if err := json.Unmarshal(expected, &want); err != nil {
		t.Fatalf("expected.json: json: %v", err)
	}
	if err := json.Unmarshal(stdout, &got); err != nil {
		t.Errorf("stdout is not one JSON document: %v", err)
		return
	}
	if err := scenario.Match(want, got); err != nil {
		t.Errorf("json: %v", err)
	}
}

// requests lists what every mock received, for the report of a failed run.
func (m *mocks) requests() string {
	var b strings.Builder
	for _, mock := range []struct {
		name     string
		requests []sequence.Request
	}{
		{"github", m.github.Requests()},
		{"circleci", m.circleci.Requests()},
		{"registry", m.registry.Requests()},
		{"privateRegistry", m.privateRegistry.Requests()},
		{"muster", m.muster.Requests()},
	} {
		for _, r := range mock.requests {
			fmt.Fprintf(&b, "  %s: %s\n", mock.name, r)
		}
	}
	if b.Len() == 0 {
		return "  none\n"
	}
	return b.String()
}

// openURL is how devctl asks the person to open a page: "<what>: open <url> ...".
var openURL = regexp.MustCompile(`\bopen (https?://\S+)`)

// human plays the person at the browser for a scenario with browser: true:
// every URL devctl asks to open on stderr is fetched, following redirects, so
// the muster mock's authorization endpoint lands its code on devctl's
// loopback callback the way a signed-in person's browser would. Only
// loopback URLs are fetched; a scenario's text may name any other.
type human struct {
	mu   sync.Mutex
	rest string
	wg   sync.WaitGroup
}

func (h *human) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rest += string(p)
	for {
		i := strings.IndexByte(h.rest, '\n')
		if i < 0 {
			break
		}
		line := h.rest[:i]
		h.rest = h.rest[i+1:]
		if m := openURL.FindStringSubmatch(line); m != nil {
			h.visit(m[1])
		}
	}
	return len(p), nil
}

func (h *human) visit(raw string) {
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	if host := u.Hostname(); host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return
	}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		resp, err := http.Get(raw) //nolint:gosec // G107: the URL is one devctl printed for the person, on the loopback
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
}

// wait lets every page load finish before the scenario ends.
func (h *human) wait() {
	h.wg.Wait()
}
