package makefile_test

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	makefilecmd "github.com/giantswarm/devctl/v8/cmd/gen/makefile"
)

func makefileCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	cmd, err := makefilecmd.New(makefilecmd.Config{
		Logger: logger, Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	return cmd
}

func TestCommandGoCLIPackaging(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		cli  bool
		app  bool
	}{
		{name: "CLI", args: []string{"--flavour", "cli"}, cli: true},
		{name: "repeated flavours", args: []string{"-f", "cli", "--flavour", "app"}, cli: true, app: true},
		{name: "comma separated flavours", args: []string{"--flavour", "app,cli"}, cli: true, app: true},
		{name: "non-CLI Go", args: []string{"--flavour", "generic"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			args := append(append([]string(nil), tc.args...), "--language", "go")
			require.NoError(t, makefileCommand(t, args...).ExecuteContext(t.Context()))
			require.FileExists(t, "Makefile")
			require.FileExists(t, ".golangci.yaml")
			goMakefile, err := os.ReadFile("Makefile.gen.go.mk")
			require.NoError(t, err)
			require.Contains(t, string(goMakefile), "go test -ldflags")
			const helper = ".github/zz_generated.windows-code-signing.sh"
			if tc.cli {
				require.Contains(t, string(goMakefile), "package-windows-amd64:")
				require.Contains(t, string(goMakefile), "/bin/sh "+helper+" $(APPLICATION) $(VERSION)")
				script, err := os.ReadFile(helper)
				require.NoError(t, err)
				require.Contains(t, string(script), "#!/bin/sh")
				info, err := os.Stat(helper)
				require.NoError(t, err)
				if runtime.GOOS != "windows" {
					require.NotZero(t, info.Mode().Perm()&0o111, "the generated helper must be executable")
				}
			} else {
				require.NotContains(t, string(goMakefile), "package-windows-amd64:")
				require.NoFileExists(t, helper)
			}
			if tc.app {
				require.FileExists(t, "Makefile.gen.app.mk", "combined flags must retain the app flavour")
			} else {
				require.NoFileExists(t, "Makefile.gen.app.mk")
			}
		})
	}
}

func TestCommandRejectsInvalidInputBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		message     string
		invalidFlag bool
	}{
		{name: "missing flavours", args: []string{"--language", "go"}, message: "--flavour", invalidFlag: true},
		{name: "missing language", args: []string{"--flavour", "cli"}, message: "--language", invalidFlag: true},
		{
			name: "CLI Python with API cleanup", args: []string{"--flavour", "k8sapi,cli", "--language", "python"},
			message: `flavour "cli" is supported only for language "go"`, invalidFlag: true,
		},
		{
			name: "CLI generic", args: []string{"--flavour", "cli", "--language", "generic"},
			message: `flavour "cli" is supported only for language "go"`, invalidFlag: true,
		},
		{
			name: "extra argument", args: []string{"--flavour", "k8sapi", "--language", "go", "extra"},
			message: "unknown command",
		},
		{
			name: "unknown flavour", args: []string{"--flavour", "not-a-flavour", "--language", "go"},
			message: "flavour must be one of",
		},
		{
			name: "unknown language", args: []string{"--flavour", "k8sapi", "--language", "not-a-language"},
			message: "language must be one of",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			// These files would be overwritten or deleted by a misplaced generator
			// call; the full snapshot also detects newly created files/directories.
			for _, path := range []string{"Makefile", "Makefile.gen.go.mk", "Makefile.custom.mk"} {
				require.NoError(t, os.WriteFile(path, []byte("repository-owned fixture\n"), 0o600))
			}
			before := repositorySnapshot(t)
			err := makefileCommand(t, tc.args...).ExecuteContext(t.Context())
			require.ErrorContains(t, err, tc.message)
			require.Equal(t, tc.invalidFlag, makefilecmd.IsInvalidFlag(err))
			require.Equal(t, before, repositorySnapshot(t), "invalid input must not change the repository")
		})
	}
}

func repositorySnapshot(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			files[path] = "directory"
			return nil
		}
		content, err := os.ReadFile(path) // #nosec G122 G304 -- synthetic t.TempDir files, with no symlinks or concurrent writers.
		if err != nil {
			return err
		}
		files[path] = string(content)
		return nil
	}))
	return files
}
