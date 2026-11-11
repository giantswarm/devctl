package makefile

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
		name           string
		source         string
		missingTool    bool
		installFailure bool
		defaultBin     bool
		linterFailure  bool
		wantErr        bool
		wantLinter     bool
		wantOutput     string
	}{
		{name: "case 0: grouped imports", source: grouped, wantLinter: true},
		{name: "case 1: local imports mixed with dependencies", source: ungrouped, wantErr: true, wantOutput: "nested/file with spaces.go"},
		{name: "case 2: invalid Go source", source: "package fixture\nfunc {\n", wantErr: true, wantOutput: "expected"},
		{name: "case 3: install missing goimports", source: grouped, missingTool: true, wantLinter: true},
		{name: "case 4: install into default GOPATH bin", source: grouped, missingTool: true, defaultBin: true, wantLinter: true},
		{name: "case 5: installed tool rejects ungrouped imports", source: ungrouped, missingTool: true, wantErr: true, wantOutput: "nested/file with spaces.go"},
		{name: "case 6: installation failure", source: grouped, missingTool: true, installFailure: true, wantErr: true, wantOutput: "installation failed"},
		{name: "case 7: linter failure", source: grouped, linterFailure: true, wantErr: true, wantLinter: true, wantOutput: "linter failed"},
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
			if tc.missingTool {
				write("installed bin/.keep", "", 0o600)
				write("bin/go", `#!/bin/sh
case "$*" in
  "env GOBIN")
    if [ "$TEST_DEFAULT_BIN" != "true" ]; then printf '%s\n' "$TEST_INSTALL_BIN"; fi ;;
  "env GOPATH") printf '%s\n' "$TEST_GOPATH" ;;
  "install golang.org/x/tools/cmd/goimports@v0.50.0")
    echo install > install-ran
    if [ "$TEST_INSTALL_FAILURE" = "true" ]; then echo 'installation failed' >&2; exit 29; fi
    exec /bin/cp "$TEST_GOIMPORTS" "$GOBIN/goimports" ;;
  *) exec "$TEST_REAL_GO" "$@" ;;
esac
`, 0o700)
			} else if err := os.Symlink(goBin, filepath.Join(dir, "bin", "go")); err != nil {
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

			// #nosec G204 -- make comes from exec.LookPath; arguments are fixed test fixtures.
			cmd := exec.CommandContext(t.Context(), makeBin, "-f", in.Path, "lint",
				"APPLICATION=fixture", "MODULE="+module, "OS=linux", "SOURCES=",
				"VERSION=unused", "GITSHA1=unused", "BUILDTIMESTAMP=unused", "RACE=")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin"), "GOPROXY=off", "GOSUMDB=off",
				"TEST_INSTALL_BIN="+filepath.Join(dir, "installed bin"),
				"TEST_GOPATH="+dir, "GOBIN=", "GOPATH="+dir,
				"TEST_GOIMPORTS="+goimportsBin, "TEST_REAL_GO="+goBin,
				"TEST_DEFAULT_BIN="+strconv.FormatBool(tc.defaultBin),
				"TEST_INSTALL_FAILURE="+strconv.FormatBool(tc.installFailure))
			out, err := cmd.CombinedOutput()
			_, installErr := os.Stat(filepath.Join(dir, "install-ran"))
			if (installErr == nil) != tc.missingTool {
				t.Errorf("installation called = %v, want %v", installErr == nil, tc.missingTool)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("make lint: error = %v, want error %v\n%s", err, tc.wantErr, out)
			}
			if !strings.Contains(string(out), tc.wantOutput) {
				t.Errorf("make lint output does not contain %q:\n%s", tc.wantOutput, out)
			}
			calls, err := os.ReadFile(filepath.Join(dir, "lint-ran")) // #nosec G304 -- fixed path under t.TempDir()
			if tc.wantLinter {
				if err != nil || string(calls) != "run --timeout=15m ./...\n" {
					t.Errorf("golangci-lint calls = %q, error = %v", calls, err)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("golangci-lint ran after an import check failure: calls = %q, error = %v", calls, err)
			}
			for path, want := range map[string]string{sourcePath: tc.source, "clean.go": "package fixture\n"} {
				got, err := os.ReadFile(filepath.Join(dir, path)) // #nosec G304 -- fixed fixture paths under t.TempDir()
				if err != nil || string(got) != want {
					t.Errorf("make lint changed %s: error = %v", path, err)
				}
			}
		})
	}
}
