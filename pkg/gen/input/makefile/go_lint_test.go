package makefile

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// TestGoLintChecksImports runs the generated lint recipe with real goimports
// and an offline linter stub. Its fixtures already pass gofmt: only -local
// distinguishes the two import layouts.
func TestGoLintChecksImports(t *testing.T) {
	makeBin, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not installed")
	}
	goimportsBin, err := exec.LookPath("goimports")
	if err != nil {
		t.Skip("goimports is not installed")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}

	m, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	in := m.MakefileGenGo()[0]
	tpl, err := template.New("makefile").Parse(in.TemplateBody)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}

	const module = "example.com/acme/service/v2"
	const grouped = `package fixture

import (
	"fmt"

	"github.com/other/library"

	"example.com/acme/service/v2/client"
)

var _ = fmt.Println
var _ = library.New
var _ = client.New
`
	const ungrouped = `package fixture

import (
	"fmt"

	"example.com/acme/service/v2/client"
	"github.com/other/library"
)

var _ = fmt.Println
var _ = library.New
var _ = client.New
`
	testCases := []struct {
		name          string
		source        string
		missingTool   bool
		linterFailure bool
		wantErr       bool
		wantLinter    bool
		wantOutput    string
	}{
		{name: "case 0: grouped imports", source: grouped, wantLinter: true},
		{name: "case 1: local imports mixed with dependencies", source: ungrouped, wantErr: true, wantOutput: "nested/file with spaces.go"},
		{name: "case 2: invalid Go source", source: "package fixture\nfunc {\n", wantErr: true, wantOutput: "expected"},
		{name: "case 3: goimports missing", source: grouped, missingTool: true, wantErr: true, wantOutput: "goimports"},
		{name: "case 4: linter failure", source: grouped, linterFailure: true, wantErr: true, wantLinter: true, wantOutput: "linter failed"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.source == grouped || tc.source == ungrouped {
				formatted, err := format.Source([]byte(tc.source))
				if err != nil || string(formatted) != tc.source {
					t.Fatalf("fixture must already pass gofmt: %v", err)
				}
			}
			dir := t.TempDir()
			write := func(path, content string, mode os.FileMode) {
				t.Helper()
				p := filepath.Join(dir, path)
				if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			write(in.Path, rendered.String(), 0o600)
			write("go.mod", "module "+module+"\n\ngo 1.24\n", 0o600)
			write("clean.go", "package fixture\n", 0o600)
			const sourcePath = "nested/file with spaces.go"
			write(sourcePath, tc.source, 0o600)
			// Make evaluates the := assignments even when command-line values
			// override them. Keep their unrelated tool lookups offline and quiet.
			for _, name := range []string{"cut", "date", "git", "find", "gitsemver"} {
				write("bin/"+name, "#!/bin/sh\n", 0o700)
			}
			// goimports reads go env even when every import is already present.
			if err := os.Symlink(goBin, filepath.Join(dir, "bin", "go")); err != nil {
				t.Fatal(err)
			}
			linter := "#!/bin/sh\nprintf '%s\\n' \"$*\" > lint-ran\n"
			if tc.linterFailure {
				linter += "echo 'linter failed' >&2\nexit 23\n"
			}
			write("bin/golangci-lint", linter, 0o700)
			if !tc.missingTool {
				if err := os.Symlink(goimportsBin, filepath.Join(dir, "bin", "goimports")); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.CommandContext(t.Context(), makeBin, "-f", in.Path, "lint",
				"APPLICATION=fixture", "MODULE="+module, "OS=linux", "SOURCES=",
				"VERSION=unused", "GITSHA1=unused", "BUILDTIMESTAMP=unused", "RACE=")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin"), "GOPROXY=off", "GOSUMDB=off")
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantErr {
				t.Errorf("make lint: error = %v, want error %v\n%s", err, tc.wantErr, out)
			}
			if !strings.Contains(string(out), tc.wantOutput) {
				t.Errorf("make lint output does not contain %q:\n%s", tc.wantOutput, out)
			}
			calls, err := os.ReadFile(filepath.Join(dir, "lint-ran"))
			if tc.wantLinter {
				if err != nil || string(calls) != "run --timeout=15m ./...\n" {
					t.Errorf("golangci-lint calls = %q, error = %v", calls, err)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("golangci-lint ran after an import check failure: calls = %q, error = %v", calls, err)
			}
			for path, want := range map[string]string{sourcePath: tc.source, "clean.go": "package fixture\n"} {
				got, err := os.ReadFile(filepath.Join(dir, path))
				if err != nil || string(got) != want {
					t.Errorf("make lint changed %s: error = %v", path, err)
				}
			}
		})
	}
}
