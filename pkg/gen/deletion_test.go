package gen

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

func Test_Execute_DeleteFailureStopsInputs(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("fixture needs Unix directory permissions without root privileges")
	}
	dir := t.TempDir()
	workflows := filepath.Join(dir, "workflows")
	if err := os.Mkdir(workflows, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(workflows, "zz_generated.auto_release.yaml")
	if err := os.WriteFile(legacy, []byte("legacy workflow"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workflows, 0500); err != nil { // #nosec G302 -- test fixture
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(workflows, 0700); err != nil { // #nosec G302 -- test fixture
			t.Error(err)
		}
	})
	calledNext := false
	err := Execute(t.Context(),
		input.Input{Path: legacy, Delete: true},
		input.Input{
			Path: filepath.Join(dir, "zz_generated.next"),
			Generate: func(context.Context) ([]byte, error) {
				calledNext = true
				return []byte("next"), nil
			},
		},
	)
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("got %v, want the deletion's permission error", err)
	}
	if calledNext {
		t.Error("started another input after the deletion failed")
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "legacy workflow" { // #nosec G304 -- test fixture
		t.Errorf("failed deletion changed legacy workflow: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "zz_generated.next")); !os.IsNotExist(err) {
		t.Errorf("failed deletion was followed by another write: %v", err)
	}
}

func Test_Execute_DeleteCleanup(t *testing.T) {
	for _, kind := range []string{"missing", "file", "empty directory", "nonempty directory"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy")
			switch kind {
			case "file":
				if err := os.WriteFile(path, []byte("legacy"), 0600); err != nil {
					t.Fatal(err)
				}
			case "empty directory", "nonempty directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "nonempty directory" {
					if err := os.WriteFile(filepath.Join(path, "own-tool"), []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := Execute(t.Context(), input.Input{Path: path, Delete: true}); err != nil {
				t.Fatal(err)
			}
			if kind == "nonempty directory" {
				if data, err := os.ReadFile(filepath.Join(path, "own-tool")); err != nil || string(data) != "keep" { // #nosec G304 -- test fixture
					t.Errorf("directory cleanup must keep other tools: %q, %v", data, err)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("cleanup did not remove %s: %v", kind, err)
			}
		})
	}
}
