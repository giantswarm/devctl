package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

func Test_Execute_NativeNestedPaths(t *testing.T) {
	for _, relative := range [][]string{
		{".github", "workflows", "zz_generated.fixture.yaml"},
		{".circleci", "config.yml"},
		{".circleci", "workflows.yml"},
		{".github", "dependabot.yml"},
	} {
		t.Run(filepath.Join(relative...), func(t *testing.T) {
			parts := append([]string{t.TempDir()}, relative...)
			destination := filepath.Join(parts...)
			for _, content := range []string{"first", "updated"} {
				if err := Execute(t.Context(), input.Input{Path: destination, TemplateBody: content}); err != nil {
					t.Fatalf("generate %q: %v", content, err)
				}
				data, err := os.ReadFile(destination) // #nosec G304 -- test fixture
				if err != nil || string(data) != content {
					t.Fatalf("generated %q, %v; want %q", data, err, content)
				}
			}
		})
	}
}

func Test_Execute_NativeCircleCIPathWithExistingParent(t *testing.T) {
	// Isolate regeneration from parent creation: the conventional CircleCI
	// names must keep regenerating even when passed as native absolute paths.
	for _, name := range []string{"config.yml", "workflows.yml"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".circleci")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(dir, name)
			if err := os.WriteFile(destination, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := Execute(t.Context(), input.Input{Path: destination, TemplateBody: "updated"}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(destination) // #nosec G304 -- test fixture
			if err != nil || string(data) != "updated" {
				t.Errorf("native CircleCI output was not updated: %q, %v", data, err)
			}
		})
	}
}

func Test_Execute_NativeConfigScaffoldPreserved(t *testing.T) {
	// A conventional filename belongs to CircleCI only inside .circleci.
	// Separator normalization must not broaden ownership to other config files.
	dir := filepath.Join(t.TempDir(), "service")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(destination, []byte("repository config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Execute(t.Context(), input.Input{Path: destination, TemplateBody: "generated"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination) // #nosec G304 -- test fixture
	if err != nil || string(data) != "repository config" {
		t.Errorf("ordinary config was overwritten: %q, %v", data, err)
	}
}
