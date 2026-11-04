package nocredentials

import (
	"errors"
	"os"
	"testing"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestFailEveryStoreRejectsKeyringOperations(t *testing.T) {
	installFailingStores(t)
	store := authstore.OpenStore(agentcli.EndpointsFromEnv())
	for _, user := range []string{authstore.UserGitHub, authstore.UserCircleCI, authstore.UserMuster} {
		t.Run(user, func(t *testing.T) {
			operations := map[string]func() error{
				"get": func() error {
					_, err := store.Get(user)
					return err
				},
				"set": func() error {
					return store.Set(user, authstore.Record{Token: "fixture-token"})
				},
				"delete": func() error { return store.Delete(user) },
				"lock": func() error {
					unlock, err := store.Lock(user)
					if unlock != nil {
						unlock()
					}
					return err
				},
			}
			for operation, run := range operations {
				t.Run(operation, func(t *testing.T) {
					if err := run(); err == nil || errors.Is(err, authstore.ErrNotFound) {
						t.Fatalf("want a hard store failure, got %v", err)
					}
				})
			}
		})
	}
}

func TestFailEveryStoreRejectsKubeconfigLoad(t *testing.T) {
	installFailingStores(t)
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	// Only exercise KUBECONFIG; never inspect the host's home migration files.
	rules.MigrationRules = nil
	if _, err := rules.Load(); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want an unreadable kubeconfig, got %v", err)
	}
}

func installFailingStores(t *testing.T) {
	t.Helper()
	for _, key := range append([]string{agentcli.EnvKeyringFile, "DOCKER_CONFIG", "KUBECONFIG", "GH_TOKEN"}, authstore.GitHubEnvVars...) {
		t.Setenv(key, "synthetic")
	}
	if err := failEveryStore(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
