package archive

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/micrologger"
	"sigs.k8s.io/yaml"
)

// Version spellings accepted by the flags must reach the filesystem engine
// as a strict version, so a leading v or omitted patch cannot cause a panic.
func TestArchiveVersionInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{name: "30.1.0", valid: true},
		{name: "v30.1.0", valid: true},
		{name: "30.1", valid: true},
		{name: "v30.1", valid: true},
		{name: "latest"},
		{name: "30.1.0/../../other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			provider := filepath.Join(root, "capa")
			active := filepath.Join(provider, "v30.1.0")
			archived := filepath.Join(provider, "archived", "v30.1.0")
			for _, dir := range []string{active, filepath.Dir(archived)} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			release := []byte("apiVersion: release.giantswarm.io/v1alpha1\nkind: Release\nmetadata:\n  name: capa-30.1.0\n")
			if err := os.WriteFile(filepath.Join(active, "release.yaml"), release, 0600); err != nil {
				t.Fatal(err)
			}
			index := filepath.Join(provider, "kustomization.yaml")
			if err := os.WriteFile(index, []byte("resources:\n- v30.1.0\n- v30.0.0\n"), 0600); err != nil {
				t.Fatal(err)
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--name", tc.name, "--provider", "capa", "--releases", root})
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("archive panicked for --name %q: %v", tc.name, p)
				}
			}()
			err = cmd.Execute()
			if !tc.valid {
				if !IsInvalidFlag(err) {
					t.Fatalf("want invalid flag error, got %v", err)
				}
				if _, err := os.Stat(active); err != nil {
					t.Fatalf("invalid input changed the active release: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(archived, "release.yaml")) // #nosec G304 -- fixed fixture path under t.TempDir, independent of command input
			if err != nil || string(data) != string(release) {
				t.Fatalf("archived release: %q, %v", data, err)
			}
			if _, err := os.Stat(active); !os.IsNotExist(err) {
				t.Fatalf("active release still exists: %v", err)
			}
			data, err = os.ReadFile(index) // #nosec G304 -- fixed fixture path under t.TempDir, independent of command input
			if err != nil {
				t.Fatal(err)
			}
			var kustomization struct {
				Resources []string `json:"resources"`
			}
			if err := yaml.Unmarshal(data, &kustomization); err != nil {
				t.Fatal(err)
			}
			if len(kustomization.Resources) != 1 || kustomization.Resources[0] != "v30.0.0" {
				t.Fatalf("provider resources after archive: %v", kustomization.Resources)
			}
		})
	}
}
