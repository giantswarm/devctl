package gitops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

func openDryRun(t *testing.T) *Checkout {
	t.Helper()
	dir, _ := newPair(t)
	checkout, err := (&Flags{Dir: dir, DryRun: true}).Open(context.Background(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return checkout
}

func TestFinishDropsCleanDryRunCommits(t *testing.T) {
	checkout := openDryRun(t)
	before := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD")
	commit(t, checkout.Dir, "dry-run")
	var stderr bytes.Buffer
	if err := checkout.Finish(context.Background(), &stderr, nil); err != nil {
		t.Fatal(err)
	}
	if got := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD %s, want %s", got, before)
	}
	if !strings.Contains(stderr.String(), "+dry-run") {
		t.Errorf("stderr does not show the dry-run patch: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(checkout.Dir, "dry-run")); !os.IsNotExist(err) {
		t.Errorf("dry-run file survived: %v", err)
	}
}

func TestFinishPreservesTrackedEditsMadeDuringADryRun(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "unstaged"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			checkout := openDryRun(t)
			commit(t, checkout.Dir, "dry-run")
			head := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD")
			const work = "work saved while the command was running\n"
			file := filepath.Join(checkout.Dir, "a")
			if err := os.WriteFile(file, []byte(work), 0o600); err != nil {
				t.Fatal(err)
			}
			if staged {
				gittest.RunGit(t, checkout.Dir, "add", "a")
			}
			beforeStatus := gittest.GitOutput(t, checkout.Dir, "status", "--porcelain")
			original := microerror.Maskf(invalidFlagError, "the operation also failed")
			err := checkout.Finish(context.Background(), io.Discard, original)
			if !errors.Is(err, original) || !IsSync(err) {
				t.Errorf("got %v, want the operation error and a cleanup refusal", err)
			}
			if got := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD"); got != head {
				t.Errorf("HEAD moved from %s to %s", head, got)
			}
			got, readErr := os.ReadFile(file) //nolint:gosec // file is a fixed path inside this test's temporary checkout
			if readErr != nil || string(got) != work {
				t.Errorf("tracked edit: got %q, %v; want %q", got, readErr, work)
			}
			if got := gittest.GitOutput(t, checkout.Dir, "status", "--porcelain"); got != beforeStatus {
				t.Errorf("staging changed: got %q, want %q", got, beforeStatus)
			}
		})
	}
}

func TestFinishReportsResetFailureAlongsideTheOperationError(t *testing.T) {
	checkout := openDryRun(t)
	commit(t, checkout.Dir, "dry-run")
	head := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(checkout.Dir, ".git", "index.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	original := microerror.Maskf(invalidFlagError, "the operation also failed")
	var stderr bytes.Buffer
	err := checkout.Finish(context.Background(), &stderr, original)
	if !errors.Is(err, original) || !IsSync(err) || !strings.Contains(err.Error(), "cannot drop the commits") {
		t.Fatalf("got %v, want both the operation and reset errors", err)
	}
	if strings.Contains(stderr.String(), "dropped these commits") {
		t.Errorf("stderr claims cleanup succeeded: %s", stderr.String())
	}
	if got := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("failed reset moved HEAD from %s to %s", head, got)
	}
}

func TestFinishRefusesCleanupWhenStatusCannotBeRead(t *testing.T) {
	checkout := openDryRun(t)
	commit(t, checkout.Dir, "dry-run")
	head := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	original := microerror.Maskf(invalidFlagError, "the operation also failed")
	var stderr bytes.Buffer
	err := checkout.Finish(ctx, &stderr, original)
	if !errors.Is(err, original) || !IsSync(err) || !strings.Contains(err.Error(), "cannot check whether it is safe") {
		t.Fatalf("got %v, want the operation error and a status-check failure", err)
	}
	if strings.Contains(stderr.String(), "dropped these commits") {
		t.Errorf("stderr claims cleanup succeeded: %s", stderr.String())
	}
	if got := gittest.GitOutput(t, checkout.Dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("unreadable status moved HEAD from %s to %s", head, got)
	}
}
