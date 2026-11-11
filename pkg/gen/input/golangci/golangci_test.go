package golangci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

func TestEnsure(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "settings only",
			in: `version: "2"

linters:
  # tuned per repository
  settings:
    goconst:
      ignore-tests: true
`,
			want: `version: "2"

linters:
  enable:
    - gosec
    - goconst
  # tuned per repository
  settings:
    goconst:
      ignore-tests: true
`,
		},
		{
			name: "enable list without the linters",
			in: `version: "2"
linters:
    enable:
        - gocyclo # complexity
        - dupl
    settings:
        dupl:
            threshold: 100
`,
			want: `version: "2"
linters:
    enable:
        - gocyclo # complexity
        - dupl
        - gosec
        - goconst
    settings:
        dupl:
            threshold: 100
`,
		},
		{
			name: "enable list with one of them",
			in: `version: "2"
linters:
  enable:
  - goconst
`,
			want: `version: "2"
linters:
  enable:
  - goconst
  - gosec
`,
		},
		{
			name: "everything enabled",
			in: `version: "2"
linters:
  enable:
    - gosec
    - goconst
`,
			want: `version: "2"
linters:
  enable:
    - gosec
    - goconst
`,
		},
		{
			name: "disabled linter stays disabled",
			in: `version: "2"
linters:
  disable:
    - goconst
`,
			want: `version: "2"
linters:
  enable:
    - gosec
  disable:
    - goconst
`,
		},
		{
			name: "default all",
			in: `version: "2"
linters:
  default: all
`,
			want: `version: "2"
linters:
  default: all
`,
		},
		{
			name: "default none adds govet",
			in: `version: "2"
linters:
  default: none
  enable:
    - errcheck
`,
			want: `version: "2"
linters:
  default: none
  enable:
    - errcheck
    - gosec
    - goconst
    - govet
`,
		},
		{
			name: "empty enable",
			in: `version: "2"
linters:
  enable:
  settings: {}
`,
			want: `version: "2"
linters:
  enable:
    - gosec
    - goconst
  settings: {}
`,
		},
		{
			name: "empty linters",
			in: `version: "2"
linters:
run:
  timeout: 5m
`,
			want: `version: "2"
linters:
  enable:
    - gosec
    - goconst
run:
  timeout: 5m
`,
		},
		{
			name: "no linters",
			in:   `version: "2"`,
			want: `version: "2"

linters:
  enable:
    - gosec
    - goconst
`,
		},
		{
			name: "last item without trailing newline",
			in: `version: "2"
linters:
  enable:
    - dupl`,
			want: `version: "2"
linters:
  enable:
    - dupl
    - gosec
    - goconst
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Ensure(".golangci.yml", []byte(tc.in))
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))

			again, err := Ensure(".golangci.yml", got)
			require.NoError(t, err)
			require.Equal(t, string(got), string(again))
		})
	}
}

func TestEnsureRefusesWhatItCannotEdit(t *testing.T) {
	cases := []struct {
		name string
		path string
		in   string
	}{
		{name: "toml", path: ".golangci.toml", in: `version = "2"`},
		{name: "flow enable", path: ".golangci.yml", in: "linters:\n  enable: [dupl]\n"},
		{name: "flow linters", path: ".golangci.yml", in: "linters: {enable: [dupl]}\n"},
		{name: "invalid yaml", path: ".golangci.yaml", in: "linters: [\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Ensure(tc.path, []byte(tc.in))
			require.True(t, IsUnsupportedConfig(err), "got %v", err)
		})
	}
}

func TestNewInputKeepsTheRepositorysConfigFile(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, ".golangci.yml")
	require.NoError(t, os.WriteFile(own, []byte("version: \"2\"\nlinters:\n  enable:\n    - gosec\n    - goconst\n"), 0o600))

	in := NewInput(dir)
	require.Equal(t, own, in.Path)
	require.NoError(t, gen.Execute(t.Context(), in))

	_, err := os.Stat(filepath.Join(dir, ConfigFile))
	require.True(t, os.IsNotExist(err), "a second configuration was written next to the repository's own")
}

// TestBareRunEnablesTheLinters checks with the real golangci-lint that a bare
// run over a repository with the written configuration lints with gosec and
// goconst.
func TestBareRunEnablesTheLinters(t *testing.T) {
	bin, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint is not installed")
	}

	cases := map[string]string{
		"none":     "",
		"settings": "version: \"2\"\nlinters:\n  settings:\n    goconst:\n      min-len: 5\n",
	}
	for name, existing := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o600))
			if existing != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".golangci.yml"), []byte(existing), 0o600))
			}
			require.NoError(t, gen.Execute(t.Context(), NewInput(dir)))

			cmd := exec.CommandContext(t.Context(), bin, "linters")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))

			enabled, _, _ := strings.Cut(string(out), "Disabled by your configuration linters:")
			for _, l := range Linters {
				require.Contains(t, enabled, "\n"+l+":", "golangci-lint does not enable %s:\n%s", l, out)
			}
		})
	}
}
