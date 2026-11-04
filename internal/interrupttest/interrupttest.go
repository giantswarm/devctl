//go:build unix

// Package interrupttest runs CLI signal regressions in isolated processes.
package interrupttest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

// Signals exercises both ways a caller interrupts a wait.
var Signals = []os.Signal{syscall.SIGTERM, os.Interrupt}

// Run calls child in a subprocess and checks its real exit and sole document.
// The child gets an explicit environment and injected sources, never a login.
func Run(t *testing.T, code int, child func() error) map[string]any {
	t.Helper()
	const marker = "devctl-interrupt-test-child"
	if os.Args[len(os.Args)-1] == marker {
		os.Exit(agentcli.Exit(child()))
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "--", marker) // #nosec G204 -- re-executes this test binary from os.Executable with test-owned arguments
	cmd.Env = []string{"GOMAXPROCS=2", "GOFLAGS=-p=2"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != code {
		t.Fatalf("want process exit %d, got %v; stdout: %s; stderr: %s", code, err, &stdout, &stderr)
	}
	var doc map[string]any
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("missing document: %v; stderr: %s", err, &stderr)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout after the document: %v, %v", extra, err)
	}
	if doc["exitCode"] != float64(code) {
		t.Fatalf("document disagrees with process exit: %v", doc)
	}
	return doc
}

// Send interrupts this child during a dependency call, waiting until its
// command context observes the signal before the dependency returns.
func Send(ctx context.Context, sig os.Signal) error {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	if err := process.Signal(sig); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return errors.New("signal did not cancel the command context")
	}
}

// ReleaseSources is a generated release with available artifacts. With an
// interrupt it retains a running tag workflow before the next read is stopped.
func ReleaseSources(t *testing.T, interrupt func(context.Context) error) *releasewait.Sources {
	t.Helper()
	status := "success"
	if interrupt != nil {
		status = "running"
	}
	sources, err := releasewaittest.GeneratedRelease("o", "r", &releasewaittest.Registry{}, status)
	if err != nil {
		t.Fatal(err)
	}
	if interrupt != nil {
		open := sources.CircleCI
		sources.CircleCI = func(ctx context.Context) (releasewait.CircleCI, error) {
			client, err := open(ctx)
			return &circleCI{CircleCI: client, interrupt: interrupt}, err
		}
	}
	return sources
}

type circleCI struct {
	releasewait.CircleCI
	interrupt func(context.Context) error
	reads     int
}

func (c *circleCI) ListPipelineWorkflows(ctx context.Context, id string) ([]circleciclient.Workflow, error) {
	c.reads++
	if c.reads == 2 {
		return nil, c.interrupt(ctx)
	}
	return c.CircleCI.ListPipelineWorkflows(ctx, id)
}
