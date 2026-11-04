package gen

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

func Test_Execute_PublicationModes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions fs.FileMode
		existing    bool
	}{
		{name: "default creation", permissions: 0},
		{name: "executable creation", permissions: 0755},
		{name: "existing private file", permissions: 0755, existing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "zz_generated.fixture")
			mode := tc.permissions
			if mode == 0 {
				mode = 0644
			}
			if tc.existing {
				mode = 0600
				if err := os.WriteFile(destination, []byte("original"), mode); err != nil {
					t.Fatal(err)
				}
			}
			// Use ordinary creation as the reference so the test honors any
			// inherited umask instead of assuming the machine's configuration.
			reference := filepath.Join(dir, "reference")
			if err := os.WriteFile(reference, nil, mode); err != nil {
				t.Fatal(err)
			}
			wantInfo, err := os.Stat(reference)
			if err != nil {
				t.Fatal(err)
			}
			if err := Execute(t.Context(), input.Input{Path: destination, Permissions: tc.permissions, TemplateBody: "complete"}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(destination) // #nosec G304 -- test fixture
			if err != nil || string(data) != "complete" {
				t.Fatalf("output = %q, %v", data, err)
			}
			gotInfo, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			if gotInfo.Mode().Perm() != wantInfo.Mode().Perm() {
				t.Errorf("mode = %v, want %v", gotInfo.Mode().Perm(), wantInfo.Mode().Perm())
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 2 {
				t.Errorf("publication left temporary files: %v, %v", entries, err)
			}
		})
	}
}

func Test_Execute_PublicationSymlink(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "missing target"
		if existing {
			name = "existing target"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			if existing {
				if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			destination := filepath.Join(dir, "zz_generated.fixture")
			if err := os.Symlink("target", destination); err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("symlink creation unavailable: %v", err)
				}
				t.Fatal(err)
			}
			if err := Execute(t.Context(), input.Input{Path: destination, TemplateBody: "replacement"}); err != nil {
				t.Fatal(err)
			}
			link, err := os.Readlink(destination)
			if err != nil || link != "target" {
				t.Errorf("output symlink changed: %q, %v", link, err)
			}
			data, err := os.ReadFile(target) // #nosec G304 -- test fixture
			if err != nil || string(data) != "replacement" {
				t.Errorf("symlink target = %q, %v", data, err)
			}
		})
	}
}

func Test_Execute_PublicationReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("fixture needs Unix file permissions without root privileges")
	}
	destination := filepath.Join(t.TempDir(), "zz_generated.fixture")
	if err := os.WriteFile(destination, []byte("original"), 0400); err != nil {
		t.Fatal(err)
	}
	err := Execute(t.Context(), input.Input{Path: destination, TemplateBody: "replacement"})
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("read-only output must still be refused: %v", err)
	}
	data, err := os.ReadFile(destination) // #nosec G304 -- test fixture
	if err != nil || string(data) != "original" {
		t.Errorf("read-only output changed: %q, %v", data, err)
	}
}

func Test_Execute_PublicationSymlinkParentTraversal(t *testing.T) {
	dir := t.TempDir()
	physical := filepath.Join(dir, "physical")
	if err := os.MkdirAll(filepath.Join(physical, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("physical", "child"), filepath.Join(dir, "jump")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "zz_generated.fixture")
	// Keep the parent traversal in the link text: filepath.Join would
	// clean it before the filesystem can follow jump.
	if err := os.Symlink("jump"+string(os.PathSeparator)+".."+string(os.PathSeparator)+"target", destination); err != nil {
		t.Fatal(err)
	}
	if err := Execute(t.Context(), input.Input{Path: destination, TemplateBody: "replacement"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(physical, "target")) // #nosec G304 -- test fixture
	if err != nil || string(data) != "replacement" {
		t.Errorf("physical symlink target = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "target")); !os.IsNotExist(err) {
		t.Errorf("link traversal wrote to the wrong parent: %v", err)
	}
}
