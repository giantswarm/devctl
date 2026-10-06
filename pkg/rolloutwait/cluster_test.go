package rolloutwait

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
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

func TestOpenClusterMissingContextNamesTheCandidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)

	_, err := OpenCluster("myinstallation", ContextPrefix+"myinstallation", nil)
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
	}
	for _, want := range []string{"teleport.giantswarm.io-myinstallation does not exist", "mention myinstallation: gs-myinstallation", "--context", "tsh kube login myinstallation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "kind-lab") {
		t.Errorf("a context that does not mention the installation is no candidate: %q", err)
	}

	if _, err := OpenCluster("myinstallation", "gs-myinstallation", nil); err != nil {
		t.Errorf("an existing context passed with --context opens: %v", err)
	}
}

func TestMissingContextErrorWithoutCandidates(t *testing.T) {
	err := MissingContextError("myinstallation", ContextPrefix+"myinstallation", []string{"kind-lab"})
	if !strings.Contains(err.Error(), "no context in the kubeconfig mentions myinstallation") {
		t.Errorf("reason: %q", err)
	}
}
