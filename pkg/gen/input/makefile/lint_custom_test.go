package makefile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// TestGoLintCustomHook uses the root Makefile's real include order and runs
// make in parallel. Go, Git, version lookup and the linter are stubbed.
func TestGoLintCustomHook(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not installed")
	}

	const customCheck = "lint-custom:\n\t@printf 'custom\\n' >> \"$(LINT_CALLS)\"\n"
	testCases := []struct {
		name       string
		custom     string
		linterExit string
		wantCalls  string
		wantError  bool
	}{
		{
			name:      "case 0: no custom checks",
			wantCalls: "golangci-lint run --timeout=15m ./...\n",
		},
		{
			name:      "case 1: custom hook precedes generated lint",
			custom:    customCheck,
			wantCalls: "custom\ngolangci-lint run --timeout=15m ./...\n",
		},
		{
			name:      "case 2: failed hook stops generated lint",
			custom:    customCheck + "\t@exit 1\n",
			wantCalls: "custom\n",
			wantError: true,
		},
		{
			name:       "case 3: generated lint failure is returned",
			custom:     customCheck,
			linterExit: "1",
			wantCalls:  "custom\ngolangci-lint run --timeout=15m ./...\n",
			wantError:  true,
		},
		{
			name:      "case 4: existing custom prerequisites are retained",
			custom:    "lint: extra-check\nextra-check:\n\t@printf 'extra\\n' >> \"$(LINT_CALLS)\"\n",
			wantCalls: "extra\ngolangci-lint run --timeout=15m ./...\n",
		},
		{
			name:      "case 5: same-named recipe is shadowed but hook runs",
			custom:    "lint:\n\t@printf 'shadowed\\n' >> \"$(LINT_CALLS)\"\n" + customCheck,
			wantCalls: "custom\ngolangci-lint run --timeout=15m ./...\n",
		},
	}

	for _, flavour := range []gen.Flavour{gen.FlavourGeneric, gen.FlavourCLI} {
		for _, tc := range testCases {
			t.Run(string(flavour)+"/"+tc.name, func(t *testing.T) {
				m, err := New(Config{Flavours: gen.FlavourSlice{flavour}})
				if err != nil {
					t.Fatal(err)
				}

				dir := t.TempDir()
				write := func(path, content string, mode os.FileMode) {
					t.Helper()
					if err := os.WriteFile(filepath.Join(dir, path), []byte(content), mode); err != nil {
						t.Fatal(err)
					}
				}
				inputs := append([]input.Input{m.Makefile()}, m.MakefileGenGo()...)
				for _, in := range inputs {
					if in.Path != "Makefile" && in.Path != "Makefile.gen.go.mk" {
						continue
					}
					tpl, err := template.New(in.Path).Parse(in.TemplateBody)
					if err != nil {
						t.Fatal(err)
					}
					var rendered bytes.Buffer
					if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
						t.Fatal(err)
					}
					write(in.Path, rendered.String(), 0o600)
				}
				if tc.custom != "" {
					write("Makefile.custom.mk", tc.custom, 0o600)
				}
				// A file with the hook's name must not suppress the custom check.
				write("lint-custom", "", 0o600)
				write("calls", "", 0o600)
				if err := os.Mkdir(filepath.Join(dir, "bin"), 0o750); err != nil {
					t.Fatal(err)
				}
				write("bin/go", "#!/bin/sh\ncase \"$*\" in\n'list -m') echo example.test/project ;;\n'env GOOS') echo linux ;;\n*) exit 1 ;;\nesac\n", 0o700)
				write("bin/git", "#!/bin/sh\necho fixture\n", 0o700)
				write("bin/gitsemver", "#!/bin/sh\necho 0.0.1\n", 0o700)
				write("bin/golangci-lint", "#!/bin/sh\nprintf 'golangci-lint %s\\n' \"$*\" >> \"$LINT_CALLS\"\nexit \"${LINT_EXIT_CODE:-0}\"\n", 0o700)

				cmd := exec.Command(makePath, "--no-print-directory", "-rR", "-j2", "lint")
				cmd.Dir = dir
				cmd.Env = append(os.Environ(),
					"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
					"MAKEFLAGS=", "MFLAGS=",
					"LINT_CALLS="+filepath.Join(dir, "calls"),
					"LINT_EXIT_CODE="+tc.linterExit,
				)
				out, err := cmd.CombinedOutput()
				if (err != nil) != tc.wantError {
					t.Fatalf("make lint error = %v, want error %t\n%s", err, tc.wantError, out)
				}
				got, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.wantCalls {
					t.Errorf("calls = %q, want %q\n%s", got, tc.wantCalls, out)
				}
				if tc.custom == customCheck && strings.Contains(string(out), "overriding recipe") {
					t.Errorf("custom hook overrides a generated recipe:\n%s", out)
				}
			})
		}
	}
}
