package apptest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func Test_Run_CanceledBeforeScaffolding(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := testCommand(t)
	// If the command discards cancellation, a fake go exits successfully.
	// Never let a regression run the host's Go tool or fetch dependencies.
	fakeGo(t, "")
	err := command.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.NoDirExists(t, "tests/e2e")
}

func Test_Run_CancelsGoSetup(t *testing.T) {
	for _, stage := range []string{"get", "mod"} {
		t.Run(stage, func(t *testing.T) {
			t.Chdir(t.TempDir())
			started := make(chan struct{})
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/"+stage {
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
				}
			}))
			defer server.Close()
			// Unblock a broken implementation before server.Close waits on it.
			defer unblock()
			fakeGo(t, server.URL)
			command := testCommand(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- command.ExecuteContext(ctx) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("fake Go setup did not start")
			}
			cancel()
			select {
			case err := <-finished:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(2 * time.Second):
				t.Error("Go setup kept running after command cancellation")
				unblock()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("fake Go setup did not finish during cleanup")
				}
			}
		})
	}
}

func Test_Run_GoSetupOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail string
		want []string
	}{
		{"success", "", []string{"/get", "/mod"}},
		{"get fails before tidy", "get", []string{"/get"}},
		{"tidy fails", "mod", []string{"/get", "/mod"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			calls := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- r.URL.Path
				if r.URL.Path == "/"+tc.fail {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			fakeGo(t, server.URL)
			err := testCommand(t).Execute()
			if tc.fail == "" {
				require.NoError(t, err)
				require.FileExists(t, "tests/e2e/go.mod")
				require.FileExists(t, "tests/e2e/suites/basic/basic_suite_test.go")
			} else {
				require.Error(t, err)
			}
			close(calls)
			var got []string
			for path := range calls {
				got = append(got, path)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func testCommand(t *testing.T) *cobra.Command {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	require.NoError(t, err)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs([]string{"--app-name=fixture", "--repo-name=fixture", "--catalog=fixture"})
	return command
}

func fakeGo(t *testing.T, endpoint string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake Go launcher requires /bin/sh")
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	bin := t.TempDir()
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'"
	script := "#!/bin/sh\nexec " + quoted + " -test.run=^TestApptestGoHelper$ -- \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o700)) // #nosec G306 -- test executable in t.TempDir needs owner execute permission
	t.Setenv("PATH", bin)
	t.Setenv("DEVCTL_TEST_APPTEST_GO", "1")
	t.Setenv("DEVCTL_TEST_APPTEST_ENDPOINT", endpoint)
}

// TestApptestGoHelper is launched instead of go. Its only possible network
// peer is the synthetic endpoint supplied by fakeGo in this test process.
func TestApptestGoHelper(t *testing.T) {
	if os.Getenv("DEVCTL_TEST_APPTEST_GO") != "1" {
		return
	}
	if endpoint := os.Getenv("DEVCTL_TEST_APPTEST_ENDPOINT"); endpoint != "" {
		var stage string
		for i, arg := range os.Args {
			if arg == "--" && i+1 < len(os.Args) {
				stage = os.Args[i+1]
				break
			}
		}
		client := &http.Client{Timeout: 10 * time.Second}
		response, err := client.Get(endpoint + "/" + stage) // #nosec G704 -- fakeGo supplies only the httptest server's address
		if err != nil {
			os.Exit(1)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
	}
	os.Exit(0)
}
