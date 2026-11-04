package create

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// Unsupported formats are rejected even without a login. The default and
// both documented formats must still reach authentication.
func TestCreateOutputValidationBeforeAuthentication(t *testing.T) {
	t.Setenv("CI", "1")
	for _, name := range authstore.GitHubEnvVars {
		t.Setenv(name, "")
	}
	for _, tc := range []struct {
		name  string
		args  []string
		valid bool
	}{
		{name: "default", valid: true},
		{name: "text", args: []string{"--output", "text"}, valid: true},
		{name: "markdown", args: []string{"--output", "markdown"}, valid: true},
		{name: "unsupported", args: []string{"--output", "json"}},
		{name: "empty", args: []string{"--output="}},
		{name: "case typo", args: []string{"--output", "Markdown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger, err := micrologger.New(micrologger.Config{})
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append([]string{"--name", "v30.0.1", "--base", "v30.0.0", "--provider", "capa"}, tc.args...))
			err = cmd.Execute()
			if tc.valid {
				if !errors.Is(err, authstore.ErrAuthRequired) {
					t.Fatalf("valid output returned %v, want the CI authentication error", err)
				}
			} else if !IsInvalidFlag(err) || !strings.Contains(err.Error(), "--output must be text or markdown") {
				t.Fatalf("invalid output returned %v, want an invalid --output error naming the formats", err)
			}
		})
	}
}

func TestInvalidOutputPreservesExistingRelease(t *testing.T) {
	t.Setenv("CI", "1")
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-output-token")
	dir := t.TempDir()
	provider := filepath.Join(dir, "capa")
	files := map[string]string{
		"v30.0.0/release.yaml": "apiVersion: release.giantswarm.io/v1alpha1\nkind: Release\nmetadata:\n  name: capa-30.0.0\nspec:\n  state: active\n",
		"v30.0.1/keep.txt":     "existing release content\n",
		"kustomization.yaml":   "resources:\n- v30.0.0\n",
		"releases.json":        "{\"releases\":[]}\n",
	}
	for name, content := range files {
		path := filepath.Join(provider, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--name", "v30.0.1", "--base", "v30.0.0", "--provider", "capa", "--releases", dir, "--bumpall", "--yes", "--overwrite", "--output", "json"})
	err = cmd.Execute()
	if !IsInvalidFlag(err) || !strings.Contains(err.Error(), "--output") {
		t.Errorf("invalid output returned %v, want an invalid --output error", err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(provider, name))
		if err != nil || string(got) != want {
			t.Errorf("invalid output changed %s (read error: %v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(provider, "v30.0.1", "release.yaml")); !os.IsNotExist(err) {
		t.Errorf("invalid output wrote release.yaml (stat error: %v)", err)
	}
}
