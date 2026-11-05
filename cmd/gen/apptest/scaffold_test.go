package apptest

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// Two initializers must not both acquire permission to write the same suite.
// Pause the first at its scaffolding log so the second can reach the same
// destination before the first writes any templates.
func Test_Run_ConcurrentScaffoldingDoesNotOverwrite(t *testing.T) {
	t.Chdir(t.TempDir())
	calls := scaffoldFakeGo(t)
	const rootModule = "module fixture\n\ngo 1.21\n"
	require.NoError(t, os.WriteFile("go.mod", []byte(rootModule), 0o600))
	logger := scaffoldLogger(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	pausedLogger := &scaffoldPauseLogger{Logger: logger, entered: entered, release: release}
	first := scaffoldCommand(t, pausedLogger, "first")
	finished := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		finished <- first.Execute()
	}()
	defer func() {
		unblock()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("initializer did not stop during cleanup")
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first initializer did not reach scaffolding")
	}

	secondErr := scaffoldCommand(t, logger, "second").Execute()
	const edited = "// user edited the newly scaffolded suite\n"
	if secondErr == nil {
		// A successful second call would let its caller start editing tests.
		require.NoError(t, os.WriteFile("tests/e2e/suites/basic/basic_suite_test.go", []byte(edited), 0o600))
	}
	unblock()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("first initializer did not finish")
	}
	if secondErr == nil {
		content, err := os.ReadFile("tests/e2e/suites/basic/basic_suite_test.go")
		require.NoError(t, err)
		t.Errorf("competing initializer succeeded; the first overwrote its user's edit: %t", string(content) != edited)
	} else {
		require.ErrorContains(t, secondErr, "already exists")
	}
	log, err := os.ReadFile(calls) // #nosec G304 -- synthetic call log in t.TempDir
	require.NoError(t, err)
	workingDir, err := filepath.Abs("tests/e2e")
	require.NoError(t, err)
	require.Equal(t, workingDir+"|get -u github.com/giantswarm/apptest-framework\n"+workingDir+"|mod tidy\n", string(log))
	rootContent, err := os.ReadFile("go.mod")
	require.NoError(t, err)
	require.Equal(t, rootModule, string(rootContent))
	config, err := os.ReadFile("tests/e2e/config.yaml")
	require.NoError(t, err)
	require.Contains(t, string(config), `appName: "first"`)
	require.FileExists(t, "tests/e2e/go.mod")
	require.FileExists(t, "tests/e2e/suites/basic/values.yaml")
}

type scaffoldPauseLogger struct {
	micrologger.Logger
	entered chan struct{}
	release chan struct{}
}

func (l *scaffoldPauseLogger) Debugf(_ context.Context, _ string, _ ...interface{}) {
	close(l.entered)
	<-l.release
}

func scaffoldLogger(t *testing.T) micrologger.Logger {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	return logger
}

func scaffoldCommand(t *testing.T, logger micrologger.Logger, app string) *cobra.Command {
	t.Helper()
	command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	require.NoError(t, err)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs([]string{"--app-name=" + app, "--repo-name=fixture", "--catalog=fixture"})
	return command
}

func scaffoldFakeGo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake Go executable requires /bin/sh")
	}
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "go-calls")
	// PATH contains only this offline stub, so a regression cannot download
	// dependencies or invoke the host's Go tool.
	script := "#!/bin/sh\nprintf '%s|%s\\n' \"$PWD\" \"$*\" >> \"$DEVCTL_APPTEST_SCAFFOLD_CALLS\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o700)) // #nosec G306 -- executable fixture in t.TempDir
	t.Setenv("PATH", bin)
	t.Setenv("DEVCTL_APPTEST_SCAFFOLD_CALLS", calls)
	return calls
}

func Test_Run_ExistingScaffoldIsUntouched(t *testing.T) {
	for _, kind := range []string{"directory", "file", "directory symlink", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Chdir(t.TempDir())
			// No dependency tool may run when the destination is occupied.
			t.Setenv("PATH", t.TempDir())
			require.NoError(t, os.Mkdir("tests", 0o750))
			const original = "user maintained fixture\n"
			var protected, target string
			switch kind {
			case "directory":
				require.NoError(t, os.Mkdir("tests/e2e", 0o750))
				protected = "tests/e2e/config.yaml"
			case "file":
				protected = "tests/e2e"
			case "directory symlink", "dangling symlink":
				target = filepath.Join(t.TempDir(), "suite")
				if kind == "directory symlink" {
					require.NoError(t, os.Mkdir(target, 0o750))
					protected = filepath.Join(target, "config.yaml")
				}
				if err := os.Symlink(target, "tests/e2e"); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			if protected != "" {
				require.NoError(t, os.WriteFile(protected, []byte(original), 0o600))
			}
			err := scaffoldCommand(t, scaffoldLogger(t), "fixture").Execute()
			require.ErrorContains(t, err, "already exists")
			if protected != "" {
				content, err := os.ReadFile(protected) // #nosec G304 -- synthetic fixture path in t.TempDir
				require.NoError(t, err)
				require.Equal(t, original, string(content))
			}
			if strings.Contains(kind, "symlink") {
				info, err := os.Lstat("tests/e2e")
				require.NoError(t, err)
				require.NotZero(t, info.Mode()&os.ModeSymlink)
				gotTarget, err := os.Readlink("tests/e2e")
				require.NoError(t, err)
				require.Equal(t, target, gotTarget)
				if kind == "dangling symlink" {
					require.NoFileExists(t, target)
					require.NoDirExists(t, target)
				}
			}
		})
	}
}

func Test_Run_ScaffoldParentErrorIsReported(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	const original = "a file occupies the parent path\n"
	require.NoError(t, os.WriteFile("tests", []byte(original), 0o600))
	err := scaffoldCommand(t, scaffoldLogger(t), "fixture").Execute()
	var pathError *os.PathError
	require.True(t, errors.As(err, &pathError), "expected the filesystem cause, got %v", err)
	content, err := os.ReadFile("tests")
	require.NoError(t, err)
	require.Equal(t, original, string(content))
}
