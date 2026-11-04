package gen

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// Run with a per-process file-size limit so the fixture cannot affect the
// test runner, its cache or other tests. No filesystem has to be filled.
func Test_Execute_WriteFailurePreservesFile(t *testing.T) {
	const original = "committed schema"
	if len(os.Args) >= 3 && os.Args[len(os.Args)-2] == "gen-write-failure-child" {
		destination := os.Args[len(os.Args)-1]
		var limit syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
			t.Fatal(err)
		}
		if limit.Cur < 64 {
			t.Fatal("fixture requires a file-size limit of at least 64 bytes")
		}
		limit.Cur = 64
		signal.Ignore(syscall.SIGXFSZ)
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
			t.Fatal(err)
		}
		err := Execute(t.Context(), input.Input{Path: destination, TemplateBody: strings.Repeat("x", 128)})
		if !errors.Is(err, syscall.EFBIG) {
			t.Fatalf("got %v, want EFBIG from the bounded write", err)
		}
		data, err := os.ReadFile(destination) // #nosec G304 G703 -- test fixture
		if err != nil || string(data) != original {
			t.Errorf("failed publication replaced the original: %q, %v", data, err)
		}
		entries, err := os.ReadDir(filepath.Dir(destination))
		if err != nil || len(entries) != 1 {
			t.Errorf("failed publication left temporary files: %v, %v", entries, err)
		}
		return
	}

	destination := filepath.Join(t.TempDir(), "values.schema.json")
	if err := os.WriteFile(destination, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- re-executes this test binary against a synthetic temporary file.
	cmd := exec.CommandContext(t.Context(), bin, "-test.run=^Test_Execute_WriteFailurePreservesFile$", "--", "gen-write-failure-child", destination)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bounded-write child: %v\n%s", err, output)
	}
}

func Test_Execute_PublicationRestrictiveUmask(t *testing.T) {
	if len(os.Args) >= 3 && os.Args[len(os.Args)-2] == "gen-umask-child" {
		dir := os.Args[len(os.Args)-1]
		// This process contains only this fixture. The parent and every
		// other test retain their original umask.
		syscall.Umask(0077)
		for _, executable := range []bool{false, true} {
			name := "zz_generated.default"
			var permissions os.FileMode
			want := os.FileMode(0600)
			if executable {
				name = "zz_generated.executable"
				permissions = 0755
				want = 0700
			}
			destination := filepath.Join(dir, name)
			if err := Execute(t.Context(), input.Input{Path: destination, Permissions: permissions, TemplateBody: "fixture"}); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(destination) // #nosec G703 -- test fixture
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != want {
				t.Errorf("%s mode = %v, want %v", name, info.Mode().Perm(), want)
			}
		}
		return
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- re-executes this test binary with a temporary directory.
	cmd := exec.CommandContext(t.Context(), bin, "-test.run=^Test_Execute_PublicationRestrictiveUmask$", "--", "gen-umask-child", t.TempDir())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("umask child: %v\n%s", err, output)
	}
}
