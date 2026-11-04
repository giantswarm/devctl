package laptop

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type lifecycleTeleport struct {
	write   func(string) error
	private string
}

func (f *lifecycleTeleport) LoggedIn(context.Context) (bool, string, error) {
	return true, "", nil
}

func (f *lifecycleTeleport) KubeLogin(_ context.Context, _, _, path string) error {
	f.private = path
	return f.write(path)
}

type lifecycleTransport func(*http.Request) (*http.Response, error)

func (f lifecycleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenClusterExecCredentialsAfterPrivateCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the credential plugin fixture is a shell script")
	}
	machine := machineKubeconfig(t)
	plugin := writeFile(t, "credentials", `#!/bin/sh
if [ -e "$DEVCTL_TEST_PRIVATE_CONFIG" ]; then
  exit 1
fi
printf 'executed' > "$DEVCTL_TEST_PLUGIN_LOG"
printf '%s' '{"apiVersion":"client.authentication.k8s.io/v1beta1","kind":"ExecCredential","status":{"token":"test-exec-token"}}'
`)
	if err := os.Chmod(plugin, 0o700); err != nil { // #nosec G302 -- the fixture must be executable
		t.Fatal(err)
	}
	pluginLog := filepath.Join(t.TempDir(), "plugin.log")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/apis/apps/v1/namespaces/default/deployments" {
			t.Errorf("unexpected cluster request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-exec-token" {
			t.Error("request did not authenticate with the fixture's exec credentials")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"apps/v1","kind":"DeploymentList","items":[{"metadata":{"name":"from-private-context"}}]}`))
	}))
	defer server.Close()
	teleport := &lifecycleTeleport{write: func(path string) error {
		info, err := os.Stat(filepath.Dir(path))
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("private directory permissions = %o, want 700", info.Mode().Perm())
		}
		config := clientcmdapi.Config{
			Clusters: map[string]*clientcmdapi.Cluster{"teleport": {
				Server: server.URL,
				CertificateAuthorityData: pem.EncodeToMemory(&pem.Block{
					Type: "CERTIFICATE", Bytes: server.Certificate().Raw,
				}),
			}},
			AuthInfos: map[string]*clientcmdapi.AuthInfo{"teleport": {Exec: &clientcmdapi.ExecConfig{
				APIVersion: "client.authentication.k8s.io/v1beta1",
				Command:    plugin,
				Env: []clientcmdapi.ExecEnvVar{
					{Name: "DEVCTL_TEST_PRIVATE_CONFIG", Value: path},
					{Name: "DEVCTL_TEST_PLUGIN_LOG", Value: pluginLog},
				},
			}}},
			Contexts: map[string]*clientcmdapi.Context{teleportContext: {Cluster: "teleport", AuthInfo: "teleport"}},
		}
		content, err := clientcmd.Write(config)
		if err != nil {
			return err
		}
		return os.WriteFile(path, content, 0o600)
	}}
	requests := 0
	client, err := OpenCluster(t.Context(), ClusterConfig{
		Installation: "myinstallation",
		Context:      teleportContext,
		Teleport:     teleport,
		Wrap: func(base http.RoundTripper) http.RoundTripper {
			return lifecycleTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				return base.RoundTrip(req)
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatal("opening the client must not contact the cluster")
	}
	assertPrivateRemoved(t, teleport.private)
	if _, err := os.Stat(pluginLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential plugin must run only when a request needs it: %v", err)
	}
	deployments, err := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("default").List(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("client must work after private cleanup: %v", err)
	}
	if len(deployments.Items) != 1 || deployments.Items[0].GetName() != "from-private-context" {
		t.Fatalf("unexpected deployments from the private context: %v", deployments.Items)
	}
	if requests != 1 {
		t.Errorf("request must pass through Wrap exactly once, got %d", requests)
	}
	if content, err := os.ReadFile(pluginLog); err != nil || string(content) != "executed" { // #nosec G304 -- the test's own fixture log
		t.Errorf("credential plugin did not execute: %v", err)
	}
	assertKubeconfigUnchanged(t, machine, kubeconfig)
}

func TestOpenClusterCleansUpFailedPrivateKubeconfigs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		failure error
		want    string
	}{
		{name: "partial write and login failure", content: teleportKubeconfig, failure: errors.New("synthetic login failure"), want: "synthetic login failure"},
		{name: "malformed output", content: "contexts: [", want: "loading the kubeconfig tsh"},
		{name: "missing context", content: kubeconfig, want: "wrote no context"},
		{name: "invalid context", content: strings.ReplaceAll(teleportKubeconfig, "cluster: teleport", "cluster: absent"), want: "kube context " + teleportContext},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine := machineKubeconfig(t)
			own := writeFile(t, "own", kubeconfig)
			teleport := &lifecycleTeleport{write: func(path string) error {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					return err
				}
				return tc.failure
			}}
			client, err := OpenCluster(t.Context(), ClusterConfig{
				Installation: "myinstallation", Context: teleportContext, Kubeconfig: own, Teleport: teleport,
			})
			if client != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want failure containing %q and no client, got client=%T, err=%v", tc.want, client, err)
			}
			assertPrivateRemoved(t, teleport.private)
			assertKubeconfigUnchanged(t, machine, kubeconfig)
			assertKubeconfigUnchanged(t, own, kubeconfig)
		})
	}
}

func assertPrivateRemoved(t *testing.T, private string) {
	t.Helper()
	if private == "" {
		t.Fatal("fallback must allocate a private kubeconfig")
	}
	if _, err := os.Stat(filepath.Dir(private)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("private kubeconfig directory must be removed: %v", err)
	}
}

func assertKubeconfigUnchanged(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path) // #nosec G304 -- a kubeconfig in the test's own temporary directory
	if err != nil || string(content) != want {
		t.Errorf("user kubeconfig must stay unchanged: %v", err)
	}
}
