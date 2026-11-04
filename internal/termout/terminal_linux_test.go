//go:build linux

package termout_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/fatih/color"
	"golang.org/x/sys/unix"

	"github.com/giantswarm/devctl/v8/internal/termout"
)

// A separate process gives stdout and stderr different destinations before
// fatih/color initializes its stdout-derived NoColor setting.
func TestPrefixesTerminalStderr(t *testing.T) {
	terminal := privateTerminal(t)
	for _, tc := range []struct {
		name    string
		noColor string
		want    string
	}{
		{"color with redirected stdout", "", "\x1b[31mError\x1b[0m|\x1b[33mWarning\x1b[0m"},
		{"NO_COLOR one", "1", "Error|Warning"},
		{"NO_COLOR zero", "0", "Error|Warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrefixesTerminalHelper$")
			cmd.Env = []string{
				"DEVCTL_TERMOUT_TEST_HELPER=1", "TERM=xterm-256color",
				"NO_COLOR=" + tc.noColor, "GOMAXPROCS=2", "GOFLAGS=-p=2",
			}
			cmd.Stderr = terminal
			got, err := cmd.Output()
			if err != nil {
				t.Fatalf("terminal helper: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("prefixes = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrefixesTerminalHelper(t *testing.T) {
	if os.Getenv("DEVCTL_TERMOUT_TEST_HELPER") != "1" {
		return
	}
	if !color.NoColor {
		t.Fatal("helper must have color disabled globally by redirected stdout")
	}
	_, _ = fmt.Fprintf(os.Stdout, "%s|%s", termout.Error(os.Stderr), termout.Warning(os.Stderr))
	os.Exit(0)
}

// Allocate our own PTY instead of opening a person's controlling terminal.
func privateTerminal(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return slave
}
