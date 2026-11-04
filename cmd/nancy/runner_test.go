package nancy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const fixtureToken = "synthetic-oss-index-token"

func TestScan(t *testing.T) {
	for i, tc := range []struct {
		name string
		mode string
		code int
	}{
		{name: "clean scan", mode: "clean"},
		{name: "vulnerability fails", mode: "vulnerable", code: 1},
		{name: "list failure cannot pass as a clean scan", mode: "list-fail", code: 13},
		{name: "scanner outage stays a failure", mode: "outage", code: 42},
		{name: "early scanner exit does not block the producer", mode: "early", code: 41},
	} {
		t.Run(fmt.Sprintf("case %d: %s", i, tc.name), func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, name := range []string{".nancy-ignore", ".nancy-ignore.generated"} {
				if err := os.WriteFile(name, []byte("fixture ignore\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// None of these inherited credentials may override the keychain.
			t.Setenv("OSSI_USERNAME", "inherited-user")
			t.Setenv("OSSI_TOKEN", "inherited-token")
			t.Setenv("GUIDE_TOKEN", "inherited-guide-token")
			t.Setenv("CGO_ENABLED", "1")
			var stdout, stderr bytes.Buffer
			r := runner{
				getRecord: func(user string) (authstore.Record, error) {
					if user != "ossindex" {
						t.Fatalf("keychain user = %q", user)
					}
					return authstore.Record{Login: "fixture@example.test", Token: fixtureToken}, nil
				},
				command: helperCommand(tc.mode, ""),
				stdout:  &stdout,
				stderr:  &stderr,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := r.run(ctx)
			assertExit(t, err, tc.code)
			if tc.mode != "early" {
				if stdout.String() != "scan stdout\n" || !strings.Contains(stderr.String(), "scan stderr\n") {
					t.Fatalf("subprocess output was not preserved: stdout %q, stderr %q", stdout.String(), stderr.String())
				}
			}
			if tc.mode == "list-fail" && !strings.Contains(stderr.String(), "list failed\n") {
				t.Fatal("go list diagnostic was lost")
			}
			for _, output := range []string{stdout.String(), stderr.String()} {
				if strings.Contains(output, fixtureToken) || strings.Contains(output, "fixture@example.test") {
					t.Fatal("credentials leaked to output")
				}
			}
		})
	}
}

func TestCredentials(t *testing.T) {
	for i, tc := range []struct {
		name   string
		record authstore.Record
		err    error
	}{
		{name: "missing record", err: authstore.ErrNotFound},
		{name: "locked or malformed record", err: errors.New(fixtureToken)},
		{name: "missing username", record: authstore.Record{Token: fixtureToken}},
		{name: "missing token", record: authstore.Record{Login: "fixture@example.test"}},
		{name: "whitespace credentials", record: authstore.Record{Login: " ", Token: " "}},
		{name: "expired token", record: authstore.Record{Login: "fixture@example.test", Token: fixtureToken, ExpiresAt: time.Unix(1, 0)}},
	} {
		t.Run(fmt.Sprintf("case %d: %s", i, tc.name), func(t *testing.T) {
			// The file-store override and inherited credentials cannot bypass
			// an absent or unusable OS keychain record.
			t.Setenv("DEVCTL_KEYRING_FILE", "must-not-be-read.json")
			t.Setenv("OSSI_TOKEN", "inherited-token")
			r := runner{
				getRecord: func(string) (authstore.Record, error) { return tc.record, tc.err },
				command: func(context.Context, string, ...string) *exec.Cmd {
					t.Fatal("started a process without keychain credentials")
					return nil
				},
			}
			err := r.run(context.Background())
			var required *authstore.AuthRequiredError
			if !errors.As(err, &required) || required.ExitCode() != agentcli.ExitAuthRequired {
				t.Fatalf("error = %v, want authentication required", err)
			}
			if strings.Contains(err.Error(), fixtureToken) || !strings.Contains(err.Error(), "docs/nancy.md") {
				t.Fatal("authentication error leaked contents or omitted setup guidance")
			}
			if required.Hint != "devctl nancy --help" {
				t.Fatal("authentication error did not suggest a runnable help command")
			}
		})
	}
}

func TestProcessStartFailure(t *testing.T) {
	for _, missing := range []string{"go", "nancy"} {
		t.Run(missing, func(t *testing.T) {
			factory := helperCommand("early", "")
			r := runner{
				getRecord: fixtureRecord,
				command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
					if name == missing {
						return exec.CommandContext(ctx, t.TempDir()+"/missing-tool") //nolint:gosec // G204: deliberately nonexistent executable under the test-owned temporary directory.
					}
					return factory(ctx, name, args...)
				},
				stdout: io.Discard, stderr: io.Discard,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.run(ctx); err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("error = %v, want missing %s", err, missing)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	t.Run("before keychain access", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := runner{getRecord: func(string) (authstore.Record, error) {
			t.Fatal("read keychain after cancellation")
			return authstore.Record{}, nil
		}}
		if err := r.run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
	})
	t.Run("both running processes are reaped", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = listener.Close() }()
		if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		r := runner{getRecord: fixtureRecord, command: helperCommand("cancel", listener.Addr().String()), stdout: io.Discard, stderr: io.Discard}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- r.run(ctx) }()
		for range 2 {
			conn, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
		}
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("canceled scan succeeded")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("cancellation did not reap both processes")
		}
	})
}

func fixtureRecord(string) (authstore.Record, error) {
	return authstore.Record{Login: "fixture@example.test", Token: fixtureToken}, nil
}

func helperCommand(mode, address string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		argv := append([]string{"-test.run=^TestNancyProcess$", "--", name, mode, address}, args...)
		return exec.CommandContext(ctx, os.Args[0], argv...) //nolint:gosec // G204: rerun this test binary with synthetic fixture arguments.
	}
}

// The subprocess validates the interface to the actual Go and nancy programs.
// No credential daemon, external scanner, module fetch or network API is used.
func TestNancyProcess(t *testing.T) {
	if len(os.Args) < 6 || os.Args[2] != "--" {
		return
	}
	name, mode, address, args := os.Args[3], os.Args[4], os.Args[5], os.Args[6:]
	if mode == "cancel" {
		conn, err := net.Dial("tcp", address) //nolint:gosec // G704: address is the parent test's loopback listener, passed to this fixture subprocess.
		if err != nil {
			os.Exit(90)
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.Copy(io.Discard, conn)
		os.Exit(91)
	}
	if os.Getenv("CGO_ENABLED") != "0" || os.Getenv("GUIDE_TOKEN") != "" {
		os.Exit(92)
	}
	if name == "go" {
		if !slices.Equal(args, []string{"list", "-json", "-deps", "./..."}) || os.Getenv("OSSI_USERNAME") != "" || os.Getenv("OSSI_TOKEN") != "" {
			os.Exit(93)
		}
		if mode == "early" {
			_, _ = io.CopyN(os.Stdout, strings.NewReader(strings.Repeat("x", 4<<20)), 4<<20)
			os.Exit(0)
		}
		fmt.Fprint(os.Stdout, `{"ImportPath":"example.test/dependency","Module":{"Path":"example.test/dependency","Version":"v1.0.0"}}`)
		if mode == "list-fail" {
			fmt.Fprintln(os.Stderr, "list failed")
			os.Exit(13)
		}
		os.Exit(0)
	}
	home, err := os.UserHomeDir()
	if err != nil || !slices.Equal(args, []string{"sleuth", "--guide-token=", "--skip-update-check", "--db-cache-path", filepath.Join(home, ".cache", "nancy"), "--quiet", "--exclude-vulnerability-file", "./.nancy-ignore", "--additional-exclude-vulnerability-files", "./.nancy-ignore.generated"}) {
		os.Exit(94)
	}
	if os.Getenv("OSSI_USERNAME") != "fixture@example.test" || os.Getenv("OSSI_TOKEN") != fixtureToken {
		os.Exit(95)
	}
	if mode == "early" {
		os.Exit(41)
	}
	for _, name := range []string{".nancy-ignore", ".nancy-ignore.generated"} {
		b, err := os.ReadFile(name) //nolint:gosec // G304: name is one of the two literal ignore-file paths above.
		if err != nil || string(b) != "fixture ignore\n" {
			os.Exit(96)
		}
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil || !bytes.Contains(data, []byte(`"Version":"v1.0.0"`)) {
		os.Exit(97)
	}
	fmt.Fprintln(os.Stdout, "scan stdout")
	fmt.Fprintln(os.Stderr, "scan stderr")
	switch mode {
	case "vulnerable":
		os.Exit(1)
	case "outage":
		os.Exit(42)
	default:
		os.Exit(0)
	}
}

func assertExit(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil && want == 0 {
		return
	}
	var exit *agentcli.ExitError
	if !errors.As(err, &exit) || exit.Code != want {
		t.Fatalf("error = %v, want exit %d", err, want)
	}
}
