package laptop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	t.Setenv("KUBECONFIG", path)
	return path
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
