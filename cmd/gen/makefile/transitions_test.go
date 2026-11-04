package makefile_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"

	makefilecmd "github.com/giantswarm/devctl/v8/cmd/gen/makefile"
)

func runMakefileTransitionCommand(t *testing.T, flavours, language string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	cmd, err := makefilecmd.New(makefilecmd.Config{
		Logger: logger, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--flavour", flavours, "--language", language})
	return cmd.ExecuteContext(t.Context())
}

func TestCommandRemovesObsoleteMakefiles(t *testing.T) {
	for _, tc := range []struct {
		name      string
		before    string
		language  string
		after     string
		afterLang string
		removed   []string
		kept      []string
	}{
		{
			name:   "chart tests stop after removing app",
			before: "app", language: "go", after: "generic", afterLang: "go",
			removed: []string{"Makefile.gen.app.mk"},
			kept:    []string{"Makefile.gen.go.mk", ".golangci.yaml"},
		},
		{
			name:   "remove API schema and Go targets",
			before: "app,k8sapi,cluster-app", language: "go", after: "app", afterLang: "generic",
			removed: []string{"Makefile.gen.k8sapi.mk", "Makefile.gen.cluster_app.mk", "Makefile.gen.go.mk"},
			kept:    []string{"Makefile.gen.app.mk", "hack/boilerplate.go.txt", ".golangci.yaml"},
		},
		{
			name:   "remove Chainsaw targets but keep repository scaffolding",
			before: "app", language: "kyverno-policy", after: "app", afterLang: "generic",
			removed: []string{"Makefile.gen.chainsaw.mk"},
			kept: []string{
				"Makefile.gen.app.mk", "hack/chainsaw-extra-resources.sh",
				"tests/chainsaw/values.yaml", "tests/chainsaw/check-policy-ready/chainsaw-test.yaml",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, runMakefileTransitionCommand(t, tc.before, tc.language))
			for _, path := range tc.removed {
				require.FileExists(t, path)
			}
			// Repository-owned includes must survive along with retained scaffolding.
			const custom = "test:\n\t@echo repository tests\n"
			require.NoError(t, os.WriteFile("Makefile.local.mk", []byte(custom), 0o600))
			kept := map[string]string{"Makefile.local.mk": custom}
			for _, path := range tc.kept {
				content, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths inside t.TempDir.
				require.NoError(t, err)
				kept[path] = string(content)
			}

			require.NoError(t, runMakefileTransitionCommand(t, tc.after, tc.afterLang))
			for _, path := range tc.removed {
				require.NoFileExists(t, path, "obsolete targets are still included by Makefile")
			}
			for path, want := range kept {
				got, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths inside t.TempDir.
				require.NoError(t, err)
				require.Equal(t, want, string(got), path)
			}
		})
	}
}

func TestCommandForkKeepsExistingMakefiles(t *testing.T) {
	t.Chdir(t.TempDir())
	const original = "test:\n\t@echo carried upstream test\n"
	for _, path := range []string{"Makefile", "Makefile.gen.app.mk", "Makefile.gen.go.mk"} {
		require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
	}
	require.NoError(t, runMakefileTransitionCommand(t, "fork", "generic"))
	for _, path := range []string{"Makefile", "Makefile.gen.app.mk", "Makefile.gen.go.mk"} {
		got, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths inside t.TempDir.
		require.NoError(t, err)
		require.Equal(t, original, string(got), path)
	}
}

func TestCommandReportsObsoleteMakefileRemovalFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	const stale = "Makefile.gen.go.mk"
	require.NoError(t, os.Mkdir(stale, 0o750))
	marker := filepath.Join(stale, "keep")
	require.NoError(t, os.WriteFile(marker, []byte("repository data"), 0o600))
	err := runMakefileTransitionCommand(t, "app", "generic")
	require.ErrorContains(t, err, stale)
	require.FileExists(t, marker, "cleanup must not recursively remove directories")
}

func TestCommandRemovalOfLastIncludeKeepsHelp(t *testing.T) {
	makeBin, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not installed")
	}
	t.Chdir(t.TempDir())
	require.NoError(t, runMakefileTransitionCommand(t, "app", "generic"))
	require.NoError(t, runMakefileTransitionCommand(t, "generic", "generic"))
	require.NoFileExists(t, "Makefile.gen.app.mk")
	// There are no recipes for tools or services in this help-only repository.
	out, err := exec.CommandContext(t.Context(), makeBin, "help").CombinedOutput() // #nosec G204 -- make from PATH runs only the generated help target in t.TempDir.
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "Display this help.")
}

func TestCommandKeepsObsoleteIncludesWhenGenerationFails(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, runMakefileTransitionCommand(t, "app", "go"))
	before, err := os.ReadFile("Makefile.gen.app.mk")
	require.NoError(t, err)
	// The Go linter input refuses configurations it cannot edit safely.
	require.NoError(t, os.WriteFile(".golangci.yaml", []byte("linters:\n  enable: [dupl]\n"), 0o600))
	err = runMakefileTransitionCommand(t, "generic", "go")
	require.ErrorContains(t, err, ".golangci.yaml")
	after, err := os.ReadFile("Makefile.gen.app.mk")
	require.NoError(t, err)
	require.Equal(t, before, after, "cleanup must follow successful generation")
}
