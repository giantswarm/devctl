package gitops

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestOverlappingCheckoutOperationsCannotPublishADryRun(t *testing.T) {
	dir, _ := newPair(t)
	ctx := context.Background()
	before := gittest.GitOutput(t, dir, "rev-parse", "origin/main")
	dry, err := (&Flags{Dir: dir, DryRun: true}).Open(ctx, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dry.Close() })
	// Both commands would otherwise pass Open before either one commits.
	if other, err := (&Flags{Dir: dir}).Open(ctx, io.Discard); err == nil {
		_ = other.Close()
		t.Fatal("a real command opened the checkout owned by a dry run")
	}
	commit(t, dir, "dry-run-only")
	if err := dry.Finish(ctx, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if got := gittest.GitOutput(t, dir, "ls-remote", "origin", "refs/heads/main"); !strings.HasPrefix(got, before+"\t") {
		t.Fatalf("dry run moved origin: %s", got)
	}
	real, err := (&Flags{Dir: dir}).Open(ctx, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = real.Close() })
	if err := reservation.PushWithRetry(ctx, dir, false, func() error {
		commit(t, dir, "real-change")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := real.Finish(ctx, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	files := gittest.GitOutput(t, dir, "ls-tree", "--name-only", "origin/main")
	if strings.Contains(files, "dry-run-only") || !strings.Contains(files, "real-change") {
		t.Fatalf("real push contains the wrong changes: %s", files)
	}
}

func TestCheckoutOwnershipUsesTheSameLockThroughASymlink(t *testing.T) {
	dir, _ := newPair(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	owner, err := (&Flags{Dir: dir}).Open(context.Background(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if other, err := (&Flags{Dir: alias}).Open(context.Background(), io.Discard); err == nil {
		_ = other.Close()
		t.Fatal("symlink bypassed checkout ownership")
	}
	separate, _ := newPair(t)
	other, err := (&Flags{Dir: separate}).Open(context.Background(), io.Discard)
	if err != nil {
		t.Fatalf("independent checkout was blocked: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckoutLockReleasesOnFinishAndOpenFailure(t *testing.T) {
	ctx := context.Background()
	for _, dryRun := range []bool{false, true} {
		for _, operationFailed := range []bool{false, true} {
			dir, _ := newPair(t)
			checkout, err := (&Flags{Dir: dir, DryRun: dryRun}).Open(ctx, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = checkout.Close() })
			var operationErr error
			if operationFailed {
				operationErr = errors.New("operation failed")
			}
			if err := checkout.Finish(ctx, io.Discard, operationErr); !errors.Is(err, operationErr) {
				t.Fatalf("operation error changed: %v", err)
			}
			reopened, err := (&Flags{Dir: dir}).Open(ctx, io.Discard)
			if err != nil {
				t.Fatalf("Finish retained ownership: dry-run=%v failed=%v: %v", dryRun, operationFailed, err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	dir, _ := newPair(t)
	gittest.RunGit(t, dir, "switch", "-c", "wrong-branch")
	if checkout, err := (&Flags{Dir: dir}).Open(ctx, io.Discard); err == nil {
		_ = checkout.Close()
		t.Fatal("fixture should fail sync")
	}
	gittest.RunGit(t, dir, "switch", "main")
	checkout, err := (&Flags{Dir: dir, DryRun: true}).Open(ctx, io.Discard)
	if err != nil {
		t.Fatalf("failed Open retained ownership: %v", err)
	}
	t.Cleanup(func() { _ = checkout.Close() })
	// Even failed dry-run cleanup must release ownership.
	indexLock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(indexLock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkout.Finish(ctx, io.Discard, nil); err == nil {
		t.Fatal("fixture should fail cleanup")
	}
	if err := os.Remove(indexLock); err != nil {
		t.Fatal(err)
	}
	reopened, err := (&Flags{Dir: dir}).Open(ctx, io.Discard)
	if err != nil {
		t.Fatalf("failed Finish retained ownership: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

// This helper is invoked in a separate process with explicit test-only args.
func TestCheckoutLockSubprocess(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "checkout-lock-helper" {
		return
	}
	dir := os.Args[len(os.Args)-1]
	checkout, err := (&Flags{Dir: dir}).Open(context.Background(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = checkout.Close() }()
	_, _ = io.WriteString(os.Stdout, "locked\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestCheckoutLockAcrossProcessesAndReleaseAfterExit(t *testing.T) {
	dir, _ := newPair(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCheckoutLockSubprocess$", "--", "checkout-lock-helper", dir) //nolint:gosec // test binary with fixed helper arguments
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "locked" {
		t.Fatalf("helper did not acquire ownership: %s (%v)", scanner.Text(), scanner.Err())
	}
	if other, err := (&Flags{Dir: dir}).Open(ctx, io.Discard); err == nil {
		_ = other.Close()
		t.Fatal("another process bypassed checkout ownership")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waited = true
	reopened, err := (&Flags{Dir: dir}).Open(ctx, io.Discard)
	if err != nil {
		t.Fatalf("process exit retained ownership: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}
