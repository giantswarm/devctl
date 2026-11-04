//go:build !windows

// Package hanguptest runs a CLI runner in a process of its own, so delivering
// SIGHUP cannot end the test process or change its signal handling.
package hanguptest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

const envChild = "DEVCTL_TEST_HANGUP_CHILD"

// Run calls run in a child with closed stdin. The runner's mocked source calls
// hangup while the wait is in progress; the child exits with the runner's code,
// and Run returns its stdout after checking that code against wantExit.
func Run(t *testing.T, wantExit int, run func(hangup func()) error) []byte {
	t.Helper()
	if os.Getenv(envChild) == t.Name() {
		// Other tests may have ignored SIGHUP in the parent. Do not inherit
		// that disposition: the runner under test must establish its own.
		signal.Reset(syscall.SIGHUP)
		if signal.Ignored(syscall.SIGHUP) {
			t.Fatal("SIGHUP is still ignored before running the command")
		}
		sent := false
		err := run(func() {
			t.Helper()
			if sent {
				return
			}
			sent = true
			if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
				t.Fatalf("sending SIGHUP during the wait: %v", err)
			}
			// Also check the disposition so delayed signal delivery cannot
			// let a runner that exits quickly pass by accident.
			if !signal.Ignored(syscall.SIGHUP) {
				t.Fatal("the runner did not ignore SIGHUP")
			}
		})
		if !sent {
			t.Fatal("the runner never reached the mocked wait")
		}
		if t.Failed() {
			os.Exit(1)
		}
		os.Exit(agentcli.Exit(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$") // #nosec G204 G702 -- the test binary itself
	// No credentials or developer configuration reach the child. A nil
	// Stdin is the null device; every source the runner uses is a test double.
	child.Env = []string{envChild + "=" + t.Name(), agentcli.EnvTimeScale + "=0.001", "GOMAXPROCS=2", "GOFLAGS=-p=2"}
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	err := child.Run()
	gotExit := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			gotExit = exitErr.ExitCode()
		} else {
			t.Fatalf("running the child: %v", err)
		}
	}
	if ctx.Err() != nil || gotExit != wantExit {
		t.Fatalf("want exit %d after SIGHUP, got %d (%v)\nstdout:\n%s\nstderr:\n%s", wantExit, gotExit, err, stdout.String(), stderr.String())
	}
	return stdout.Bytes()
}
