package file

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/ami/internal/params"
)

func TestNewAMIInputCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "templates")
	const want = `{"1.2.3":{"test-region":"ami-synthetic"}}`
	in := NewAMIInput(params.Params{Dir: dir, AMIInfoString: want})
	if err := gen.Execute(t.Context(), in); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "aws-ami.yaml.template")) // #nosec G304 -- isolated t.TempDir destination
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("generated content = %q, want %q", got, want)
	}
}

func TestNewAMIInputDestination(t *testing.T) {
	absoluteDir := t.TempDir()
	for _, tc := range []struct{ dir, want string }{
		{".", "aws-ami.yaml.template"},
		{"templates", filepath.FromSlash("templates/aws-ami.yaml.template")},
		{"templates/../out", filepath.FromSlash("out/aws-ami.yaml.template")},
		{absoluteDir, absoluteDir + string(filepath.Separator) + "aws-ami.yaml.template"},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			got := NewAMIInput(params.Params{Dir: tc.dir, AMIInfoString: `{}`})
			if filepath.Clean(got.Path) != tc.want {
				t.Fatalf("destination = %q, want %q", got.Path, tc.want)
			}
		})
	}
}

func TestNewAMIInputWindowsDestination(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows filesystem path semantics")
	}

	// These paths are synthetic; constructing an Input must not access a share
	// or depend on the current directory of any drive.
	for _, tc := range []struct {
		name, dir, want string
	}{
		{"forward slash UNC", "//build-host/share/templates", `\\build-host\share\templates\aws-ami.yaml.template`},
		{"backslash UNC", `\\build-host\share\templates`, `\\build-host\share\templates\aws-ami.yaml.template`},
		{"drive relative", "C:", `C:aws-ami.yaml.template`},
		{"parent of drive root", "C:/templates/../..", `C:\aws-ami.yaml.template`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NewAMIInput(params.Params{Dir: tc.dir, AMIInfoString: `{}`})
			if filepath.Clean(got.Path) != tc.want {
				t.Fatalf("destination for %q = %q, want %q", tc.dir, got.Path, tc.want)
			}
		})
	}
}
