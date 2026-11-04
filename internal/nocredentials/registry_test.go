package nocredentials

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// The registry keychain also understands Podman files. A missing Docker
// config must not let an engine test read one, or silently act anonymously.
func TestFailEveryStoreRejectsRegistryFallbacks(t *testing.T) {
	for _, source := range []string{"no config", "registry auth file", "runtime directory", "config directory"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			// Every credential location is synthetic, including the home that
			// the library checks before considering DOCKER_CONFIG.
			t.Setenv("HOME", root)
			t.Setenv("USERPROFILE", root)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
			t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "runtime"))
			t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(root, "auth.json"))
			for _, key := range append([]string{agentcli.EnvKeyringFile, "DOCKER_CONFIG", "KUBECONFIG", "GH_TOKEN"}, authstore.GitHubEnvVars...) {
				t.Setenv(key, "synthetic")
			}

			var fixture string
			switch source {
			case "registry auth file":
				fixture = filepath.Join(root, "auth.json")
			case "runtime directory":
				fixture = filepath.Join(root, "runtime", "containers", "auth.json")
			case "config directory":
				fixture = filepath.Join(root, "config", "containers", "auth.json")
			}
			contents := []byte(`{"auths":{"registry.invalid":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("fixture-user:fixture-password")) + `"}}}`)
			if fixture != "" {
				if err := os.MkdirAll(filepath.Dir(fixture), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixture, contents, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if err := failEveryStore(root); err != nil {
				t.Fatal(err)
			}
			registry, err := name.NewRegistry("registry.invalid")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := authn.DefaultKeychain.Resolve(registry); err == nil {
				t.Fatal("default registry keychain answered successfully; want a hard failure even when no Docker config exists")
			}
			if fixture != "" {
				got, err := os.ReadFile(fixture) //nolint:gosec // G304: a synthetic fixture under t.TempDir
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, contents) {
					t.Error("the inherited registry fixture was changed")
				}
			}
		})
	}
}
