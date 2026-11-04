package file

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/ami/internal/params"
)

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
