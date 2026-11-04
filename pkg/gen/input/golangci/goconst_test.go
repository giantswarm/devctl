package golangci

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

func TestNewInputGoconstSettings(t *testing.T) {
	testCases := []struct {
		name     string
		existing string
		ignore   bool
	}{
		{name: "case 0: new configuration ignores test fixtures", ignore: true},
		{
			name: "case 1: existing configuration opts into test checks",
			existing: `version: "2"
linters:
  enable:
    - gosec
    - goconst
  settings:
    goconst:
      ignore-tests: false # repository policy
      min-occurrences: 5
      min-len: 5
`,
		},
		{
			name: "case 2: existing configuration keeps test fixtures ignored",
			existing: `version: "2"
linters:
  enable:
    - gosec
    - goconst
  settings:
    goconst:
      ignore-tests: true # repository policy
`,
			ignore: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ConfigFile)
			if tc.existing != "" {
				require.NoError(t, os.WriteFile(path, []byte(tc.existing), 0o600))
			}
			require.NoError(t, gen.Execute(t.Context(), NewInput(dir)))
			content, err := os.ReadFile(path) // #nosec G304 -- test fixture
			require.NoError(t, err)
			var config map[string]any
			require.NoError(t, yaml.Unmarshal(content, &config))
			linters := config["linters"].(map[string]any)
			require.Contains(t, linters["enable"], "goconst")
			settings := linters["settings"].(map[string]any)["goconst"].(map[string]any)
			require.Equal(t, tc.ignore, settings["ignore-tests"])
			if tc.existing != "" {
				require.Equal(t, tc.existing, string(content))
			}

			require.NoError(t, gen.Execute(t.Context(), NewInput(dir)))
			again, err := os.ReadFile(path) // #nosec G304 -- test fixture
			require.NoError(t, err)
			require.Equal(t, string(content), string(again))
		})
	}
}

func TestGoconstTestFixtures(t *testing.T) {
	bin, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint is not installed")
	}
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")

	testCases := []struct {
		name       string
		production string
		existing   string
		issueFile  string
	}{
		{
			name:       "case 0: test literals do not increase production counts",
			production: "func Value() string { return \"shared-fixture-value\" }\n",
		},
		{
			name: "case 1: production literals are still checked",
			production: `func First() string { return "production-value" }
func Second() string { return "production-value" }
func Third() string { return "production-value" }
`,
			issueFile: "fixture.go",
		},
		{
			name:       "case 2: repository can opt into test checks",
			production: "func Value() string { return \"shared-fixture-value\" }\n",
			existing: `version: "2"
linters:
  settings:
    goconst:
      ignore-tests: false
`,
			issueFile: "fixture_test.go",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				"go.mod":     "module example.com/fixture\n\ngo 1.22\n",
				"fixture.go": "package fixture\n\n" + tc.production,
				"fixture_test.go": `package fixture

func Fixtures() []string {
	return []string{
		"shared-fixture-value", "shared-fixture-value", "shared-fixture-value",
		"test-only-value", "test-only-value", "test-only-value",
	}
}
`,
			}
			if tc.existing != "" {
				files[".golangci.yml"] = tc.existing
			}
			for name, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
			}
			require.NoError(t, gen.Execute(t.Context(), NewInput(dir)))

			cmd := exec.CommandContext(t.Context(), bin, "run", "--enable-only=goconst", "--allow-parallel-runners", "--timeout=30s", "./...")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if tc.issueFile == "" {
				require.NoError(t, err, string(out))
				return
			}
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, string(out))
			require.Equal(t, 1, exitErr.ExitCode(), string(out))
			require.Contains(t, string(out), tc.issueFile+":")
			require.Contains(t, string(out), "(goconst)")
		})
	}
}
