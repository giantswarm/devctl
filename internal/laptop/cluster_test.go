package laptop

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

const kubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: lab
  cluster:
    server: https://127.0.0.1:6443
users:
- name: lab
  user:
    token: test
contexts:
- name: gs-myinstallation
  context:
    cluster: lab
    user: lab
- name: kind-lab
  context:
    cluster: lab
    user: lab
current-context: kind-lab
`

// teleportKubeconfig is what tsh kube login myinstallation writes.
const teleportKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: teleport
  cluster:
    server: https://127.0.0.1:3026
users:
- name: teleport
  user:
    token: test
contexts:
- name: teleport.giantswarm.io-myinstallation
  context:
    cluster: teleport
    user: teleport
current-context: teleport.giantswarm.io-myinstallation
`

const teleportContext = rolloutwait.ContextPrefix + "myinstallation"

// writeFile writes content to name in a new temporary directory.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// machineKubeconfig points KUBECONFIG at a kubeconfig without the Teleport
// context and returns its path.
func machineKubeconfig(t *testing.T) string {
	t.Helper()
	path := writeFile(t, "config", kubeconfig)
	// client-go also checks for legacy home-file migration, independently
	// of KUBECONFIG. Keep its destination in the fixture and already present.
	previousHome := clientcmd.RecommendedHomeFile
	clientcmd.RecommendedHomeFile = writeFile(t, "home-config", kubeconfig)
	t.Cleanup(func() { clientcmd.RecommendedHomeFile = previousHome })
	t.Setenv("DEVCTL_KUBECONFIG", "")
	t.Setenv("KUBECONFIG", path)
	return path
}

func TestOpenClusterMissingContextNamesSearchedFiles(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		teleport *fakeTeleport
		exit     int
	}{
		{name: "case 0: default file", source: "default", exit: agentcli.ExitUsage},
		{name: "case 1: merged files", source: "merged", exit: agentcli.ExitUsage},
		{name: "case 2: explicit file", source: "flag", exit: agentcli.ExitUsage},
		{name: "case 3: environment file", source: "env", exit: agentcli.ExitUsage},
		{name: "case 4: no tsh", source: "merged", teleport: &fakeTeleport{err: ErrNoTsh}, exit: agentcli.ExitUsage},
		{name: "case 5: login refused", source: "merged", teleport: &fakeTeleport{loggedIn: true, loginErr: errors.New("login refused")}, exit: agentcli.ExitUsage},
		{name: "case 6: not logged in", source: "merged", teleport: &fakeTeleport{}, exit: agentcli.ExitAuthRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := machineKubeconfig(t)
			home := clientcmd.RecommendedHomeFile
			private := writeFile(t, "private config", kubeconfig)
			missing := filepath.Join(t.TempDir(), "missing-config")
			t.Setenv("KUBECONFIG", strings.Join([]string{machine, private, machine, missing}, string(os.PathListSeparator)))
			c := ClusterConfig{Installation: "myinstallation", Context: teleportContext}
			if tc.teleport != nil {
				c.Teleport = tc.teleport
			}
			searched := []string{machine, private, missing}
			excluded := []string{home}
			switch tc.source {
			case "default":
				t.Setenv("KUBECONFIG", "")
				searched, excluded = []string{home}, searched
			case "flag":
				c.Kubeconfig = private
				// An invalid lower-priority file must not be read.
				t.Setenv("DEVCTL_KUBECONFIG", writeFile(t, "unused-env", "not a kubeconfig"))
				searched, excluded = []string{private}, []string{home, machine, missing}
			case "env":
				t.Setenv("DEVCTL_KUBECONFIG", private)
				searched, excluded = []string{private}, []string{home, machine, missing}
			}

			_, err := OpenCluster(context.Background(), c)
			if agentcli.Exit(err) != tc.exit {
				t.Fatalf("want exit %d, got %d (%v)", tc.exit, agentcli.Exit(err), err)
			}
			for _, want := range append([]string{teleportContext, "kubeconfig files searched:", "DEVCTL_KUBECONFIG", "--kubeconfig <file>"}, searched...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%q does not contain %q", err, want)
				}
			}
			for _, path := range excluded {
				if strings.Contains(err.Error(), path) {
					t.Errorf("reason names an unsearched file %q: %v", path, err)
				}
			}
			if strings.Count(err.Error(), machine) > 1 {
				t.Errorf("duplicate KUBECONFIG path listed more than once: %v", err)
			}
		})
	}
}

func TestOpenClusterKubeconfigLoadErrorNamesContextAndFiles(t *testing.T) {
	for _, source := range []string{"flag", "env"} {
		t.Run(source, func(t *testing.T) {
			machineKubeconfig(t)
			path := filepath.Join(t.TempDir(), "missing-config")
			c := ClusterConfig{Installation: "myinstallation", Context: teleportContext}
			if source == "flag" {
				c.Kubeconfig = path
			} else {
				t.Setenv("DEVCTL_KUBECONFIG", path)
			}
			_, err := OpenCluster(context.Background(), c)
			if agentcli.Exit(err) != agentcli.ExitUsage {
				t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
			}
			for _, want := range []string{teleportContext, path, "loading the kubeconfig"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%q does not contain %q", err, want)
				}
			}
		})
	}
}

type clusterTransport func(*http.Request) (*http.Response, error)

func (f clusterTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenClusterKubeconfigSourcePrecedence(t *testing.T) {
	cases := []struct {
		name string
		env  bool
		flag bool
		host string
	}{
		{name: "case 0: KUBECONFIG merges contexts", host: "merged.invalid"},
		{name: "case 1: DEVCTL_KUBECONFIG overrides KUBECONFIG", env: true, host: "env.invalid"},
		{name: "case 2: flag overrides both environment variables", env: true, flag: true, host: "flag.invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := machineKubeconfig(t)
			configs := map[string]string{machine: kubeconfig}
			private := func(name, host string) string {
				content := strings.ReplaceAll(teleportKubeconfig, "127.0.0.1:3026", host)
				path := writeFile(t, name, content)
				configs[path] = content
				return path
			}
			merged := private("merged", "merged.invalid")
			machineEnv := machine + string(os.PathListSeparator) + merged
			t.Setenv("KUBECONFIG", machineEnv)
			if tc.env {
				t.Setenv("DEVCTL_KUBECONFIG", private("env", "env.invalid"))
			}
			teleport := &fakeTeleport{loggedIn: true}
			c := ClusterConfig{Installation: "myinstallation", Context: teleportContext, Teleport: teleport}
			if tc.flag {
				c.Kubeconfig = private("flag", "flag.invalid")
			}
			requests := 0
			c.Wrap = func(http.RoundTripper) http.RoundTripper {
				return clusterTransport(func(r *http.Request) (*http.Response, error) {
					requests++
					if r.URL.Host != tc.host || r.Method != http.MethodGet || r.URL.Path != "/apis/helm.toolkit.fluxcd.io/v2/helmreleases" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmReleaseList","items":[]}`))}, nil
				})
			}
			client, err := OpenCluster(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Resource(schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || teleport.logins != 0 {
				t.Errorf("want one fixture request and no login, got %d requests and %d logins", requests, teleport.logins)
			}
			if os.Getenv("KUBECONFIG") != machineEnv {
				t.Error("KUBECONFIG changed")
			}
			for path, want := range configs {
				content, err := os.ReadFile(path) // #nosec G304 -- paths are files in this test's temporary directories
				if err != nil || string(content) != want {
					t.Errorf("fixture kubeconfig changed: %s (%v)", path, err)
				}
			}
		})
	}
}

func TestOpenClusterMissingContextNamesTheCandidates(t *testing.T) {
	machineKubeconfig(t)

	_, err := OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: teleportContext})
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
	}
	for _, want := range []string{"teleport.giantswarm.io-myinstallation does not exist", "mention myinstallation: gs-myinstallation", "--kubeconfig <file>", "--context", "tsh kube login myinstallation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "kind-lab") {
		t.Errorf("a context that does not mention the installation is no candidate: %q", err)
	}

	if _, err := OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: "gs-myinstallation"}); err != nil {
		t.Errorf("an existing context passed with --context opens: %v", err)
	}
}

func TestMissingContextErrorWithoutCandidates(t *testing.T) {
	err := MissingContextError("myinstallation", teleportContext, []string{"kind-lab"})
	if !strings.Contains(err.Error(), "no context in the kubeconfig mentions myinstallation") {
		t.Errorf("reason: %q", err)
	}
}

func TestOpenClusterKubeconfigFlag(t *testing.T) {
	machineKubeconfig(t)
	own := writeFile(t, "own", teleportKubeconfig)

	if _, err := OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: teleportContext, Kubeconfig: own}); err != nil {
		t.Errorf("the context of --kubeconfig opens: %v", err)
	}

	// --kubeconfig is the only file read: the contexts of KUBECONFIG are not
	// there.
	_, err := OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: "gs-myinstallation", Kubeconfig: own})
	if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(err.Error(), "gs-myinstallation does not exist") {
		t.Errorf("a context only KUBECONFIG has: want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
	}

	_, err = OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: teleportContext, Kubeconfig: filepath.Join(t.TempDir(), "missing")})
	if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(err.Error(), "loading the kubeconfig") {
		t.Errorf("a --kubeconfig that does not exist: want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
	}
}

// fakeTeleport is Teleport without tsh: KubeLogin writes teleportKubeconfig.
type fakeTeleport struct {
	loggedIn bool
	why      string
	err      error
	loginErr error

	logins     int
	kubeconfig string
}

func (f *fakeTeleport) LoggedIn(context.Context) (bool, string, error) {
	return f.loggedIn, f.why, f.err
}

func (f *fakeTeleport) KubeLogin(_ context.Context, installation, kubeContext, kubeconfig string) error {
	f.logins++
	f.kubeconfig = kubeconfig
	if f.loginErr != nil {
		return f.loginErr
	}
	if installation != "myinstallation" || kubeContext != teleportContext {
		return errors.New("unexpected login " + installation + " " + kubeContext)
	}
	return os.WriteFile(kubeconfig, []byte(teleportKubeconfig), 0o600)
}

func TestOpenClusterLogsInIntoAPrivateKubeconfig(t *testing.T) {
	for _, flagged := range []bool{false, true} {
		machine := machineKubeconfig(t)
		c := ClusterConfig{Installation: "myinstallation", Context: teleportContext}
		read := machine
		if flagged {
			c.Kubeconfig = writeFile(t, "own", kubeconfig)
			read = c.Kubeconfig
		}
		teleport := &fakeTeleport{loggedIn: true}
		c.Teleport = teleport

		client, err := OpenCluster(context.Background(), c)
		if err != nil || client == nil {
			t.Fatalf("--kubeconfig %t: want the context opened after the login, got %v", flagged, err)
		}
		if teleport.logins != 1 || teleport.kubeconfig == machine || teleport.kubeconfig == read {
			t.Errorf("--kubeconfig %t: the login goes into a private kubeconfig, got %d login(s) into %q", flagged, teleport.logins, teleport.kubeconfig)
		}
		if _, err := os.Stat(teleport.kubeconfig); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("--kubeconfig %t: the private kubeconfig is removed on return: %v", flagged, err)
		}
		if content, _ := os.ReadFile(read); string(content) != kubeconfig { // #nosec G304 -- a file of the test's own temporary directory
			t.Errorf("--kubeconfig %t: the kubeconfig read is never written:\n%s", flagged, content)
		}
	}
}

func TestOpenClusterTeleportFallbacks(t *testing.T) {
	cases := []struct {
		name     string
		context  string
		teleport *fakeTeleport
		exit     int
		reason   []string
		logins   int
	}{
		{
			name:     "not logged in is exit 8",
			context:  teleportContext,
			teleport: &fakeTeleport{why: "Not logged in."},
			exit:     agentcli.ExitAuthRequired,
			reason:   []string{"tsh is not logged in (Not logged in.)", "tsh login", "--context"},
		},
		{
			name:     "no tsh keeps the missing context",
			context:  teleportContext,
			teleport: &fakeTeleport{err: ErrNoTsh},
			exit:     agentcli.ExitUsage,
			reason:   []string{"does not exist", "gs-myinstallation", "tsh kube login myinstallation"},
		},
		{
			name:     "a failed login is a usage error",
			context:  teleportContext,
			teleport: &fakeTeleport{loggedIn: true, loginErr: errors.New("tsh kube login myinstallation: kubernetes cluster \"myinstallation\" not found")},
			exit:     agentcli.ExitUsage,
			reason:   []string{"not found", "--context"},
			logins:   1,
		},
		{
			name:     "another context never logs in",
			context:  "kind-myinstallation",
			teleport: &fakeTeleport{loggedIn: true},
			exit:     agentcli.ExitUsage,
			reason:   []string{"kind-myinstallation does not exist"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machineKubeconfig(t)
			_, err := OpenCluster(context.Background(), ClusterConfig{Installation: "myinstallation", Context: tc.context, Teleport: tc.teleport})
			if agentcli.Exit(err) != tc.exit {
				t.Fatalf("want exit %d, got %d (%v)", tc.exit, agentcli.Exit(err), err)
			}
			for _, want := range tc.reason {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%q does not contain %q", err, want)
				}
			}
			if tc.teleport.logins != tc.logins {
				t.Errorf("want %d login(s), got %d", tc.logins, tc.teleport.logins)
			}
		})
	}
}
