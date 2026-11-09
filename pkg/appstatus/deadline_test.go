package appstatus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

type deadlineFixture struct {
	LoginDelay   time.Duration
	StatusDelay  time.Duration
	LoginExit    int
	StatusExit   int
	Status       string
	StatusStderr string
	HoldPipes    bool
	HoldAddress  string
}

// Re-execute this test binary as native tsh/kubectl substitutes on every OS.
// The marker is set only for these tests, and PATH contains only their copies.
func init() {
	if os.Getenv("DEVCTL_APPSTATUS_DEADLINE_HELPER") != "1" {
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--appstatus-hold-pipes" {
		conn, err := net.DialTimeout("tcp", os.Args[2], time.Second) // #nosec G704 -- the parent test passes its own loopback listener address
		if err != nil {
			os.Exit(98)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, _ = conn.Read(make([]byte, 1))
		_ = conn.Close()
		os.Exit(0)
	}
	if err := runDeadlineHelper(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(99)
	}
	os.Exit(0)
}

func runDeadlineHelper() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(executable)
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json")) // #nosec G304 G703 -- fixture next to the test's own executable copy
	if err != nil {
		return err
	}
	var fixture deadlineFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if err := os.WriteFile(filepath.Join(dir, name+".called"), nil, 0o600); err != nil { // #nosec G703 -- marker in the test's own directory
		return err
	}
	switch name {
	case "tsh":
		time.Sleep(fixture.LoginDelay)
		os.Exit(fixture.LoginExit)
	case "kubectl":
		if fixture.HoldPipes {
			child := exec.Command(os.Args[0], "--appstatus-hold-pipes", fixture.HoldAddress) // #nosec G204 G702 -- another copy of this test helper, with a test-owned loopback address
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				return err
			}
			if err := child.Process.Release(); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprint(os.Stderr, fixture.StatusStderr)
		time.Sleep(fixture.StatusDelay)
		_, _ = fmt.Fprint(os.Stdout, fixture.Status)
		os.Exit(fixture.StatusExit)
	default:
		return fmt.Errorf("unexpected helper name %q", name)
	}
	return nil
}

func installDeadlineHelpers(t *testing.T, fixture deadlineFixture) string {
	t.Helper()
	dir := t.TempDir()
	if fixture.HoldPipes {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		fixture.HoldAddress = listener.Addr().String()
		release, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			<-release
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			_, _ = conn.Write([]byte{1})
			_, _ = io.Copy(io.Discard, conn)
		}()
		t.Cleanup(func() {
			close(release)
			_ = listener.Close()
			<-done
		})
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable) // #nosec G304 G703 -- only the running test executable
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tsh", "kubectl"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil { // #nosec G306 G703 -- native executable fixture in t.TempDir
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("DEVCTL_APPSTATUS_DEADLINE_HELPER", "1")
	return dir
}

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

func TestWaitForAppDeploymentContext(t *testing.T) {
	cases := []struct {
		name          string
		fixture       deadlineFixture
		parent        string
		timeout       time.Duration
		want          error
		noCommands    bool
		cancelOnWrite bool
	}{
		{name: "already canceled", parent: "canceled", timeout: time.Hour, want: context.Canceled, noCommands: true},
		{name: "expired parent", parent: "expired", timeout: time.Hour, want: context.DeadlineExceeded, noCommands: true},
		{name: "zero timeout", want: context.DeadlineExceeded, noCommands: true},
		{name: "negative timeout", timeout: -time.Second, want: context.DeadlineExceeded, noCommands: true},
		{name: "cancel login", parent: "cancel", fixture: deadlineFixture{LoginDelay: 3 * time.Second}, timeout: time.Hour, want: context.Canceled},
		{name: "earlier parent deadline", parent: "deadline", fixture: deadlineFixture{LoginDelay: 3 * time.Second}, timeout: time.Hour, want: context.DeadlineExceeded},
		{name: "timeout includes login", fixture: deadlineFixture{LoginDelay: 3 * time.Second}, timeout: 150 * time.Millisecond, want: context.DeadlineExceeded},
		{name: "cancel status read", parent: "cancel", fixture: deadlineFixture{StatusDelay: 3 * time.Second}, timeout: time.Hour, want: context.Canceled},
		{name: "timeout stops status read", fixture: deadlineFixture{StatusDelay: 3 * time.Second}, timeout: 150 * time.Millisecond, want: context.DeadlineExceeded},
		{name: "cancel retry wait", parent: "cancel", fixture: deadlineFixture{Status: "pending-install"}, timeout: time.Hour, want: context.Canceled},
		{name: "canceled deployed response", fixture: deadlineFixture{StatusStderr: "cancel now"}, timeout: time.Hour, want: context.Canceled, cancelOnWrite: true},
		{name: "inherited output pipes", fixture: deadlineFixture{HoldPipes: true}, timeout: 150 * time.Millisecond, want: context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.fixture.Status == "" {
				tc.fixture.Status = "deployed"
			}
			dir := installDeadlineHelpers(t, tc.fixture)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch tc.parent {
			case "canceled":
				cancel()
			case "expired":
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			case "cancel":
				timer := time.AfterFunc(150*time.Millisecond, cancel)
				defer timer.Stop()
			case "deadline":
				ctx, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
				defer cancel()
			}
			stderr := io.Writer(io.Discard)
			if tc.cancelOnWrite {
				stderr = cancelWriter{cancel: cancel}
			}
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			client, err := New(Config{Logger: logger, Stderr: stderr})
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			err = client.WaitForAppDeployment(ctx, "workload-example", "org-example", "target-cluster", tc.timeout)
			if !errors.Is(err, tc.want) {
				t.Errorf("want context cause %v, got %v", tc.want, err)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("wait took %v; subprocesses must stop within the context and pipe-drain bounds", elapsed)
			}
			if tc.noCommands {
				for _, name := range []string{"tsh", "kubectl"} {
					if _, err := os.Stat(filepath.Join(dir, name+".called")); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("%s started despite an expired context: %v", name, err)
					}
				}
			}
		})
	}
}

func TestWaitForAppDeploymentCommandOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		fixture deadlineFixture
		want    error
	}{
		{name: "deployed", fixture: deadlineFixture{Status: "deployed"}},
		{name: "failed read with deployed stdout", fixture: deadlineFixture{Status: "deployed", StatusExit: 1}, want: context.DeadlineExceeded},
		{name: "pending", fixture: deadlineFixture{Status: "pending-install"}, want: context.DeadlineExceeded},
		{name: "empty status", want: context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installDeadlineHelpers(t, tc.fixture)
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			client, err := New(Config{Logger: logger, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			timeout := 150 * time.Millisecond
			if tc.want == nil {
				timeout = 5 * time.Second
			}
			err = client.WaitForAppDeployment(t.Context(), "workload-example", "org-example", "target-cluster", timeout)
			if !errors.Is(err, tc.want) {
				t.Errorf("want %v, got %v", tc.want, err)
			}
		})
	}
}
