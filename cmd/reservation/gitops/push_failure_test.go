package gitops

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestExplainRecognizesPushFailureAfterAnotherSweepError(t *testing.T) {
	dir, _ := newPair(t)
	checkout, err := (&Flags{Dir: dir}).Open(context.Background(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = checkout.Finish(context.Background(), io.Discard, nil) })
	gittest.RunGit(t, dir, "remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), "missing"))
	pushErr := reservation.PushWithRetry(context.Background(), dir, false, func() error {
		commit(t, dir, "pending")
		return nil
	})
	if !reservation.IsPushFailed(pushErr) {
		t.Fatalf("fixture did not produce a push failure: %v", pushErr)
	}
	_, earlier := reservation.List(reservation.ListRequest{})
	if earlier == nil {
		t.Fatal("fixture did not produce an earlier reservation error")
	}
	explained := checkout.Explain(errors.Join(earlier, pushErr))
	if !IsPushFailed(explained) {
		t.Fatalf("push failure was hidden by the earlier error: %v", explained)
	}
	for _, want := range []string{earlier.Error(), pushErr.Error(), resetCommand(dir), "NOT in effect"} {
		if !strings.Contains(explained.Error(), want) {
			t.Errorf("diagnostic does not contain %q: %v", want, explained)
		}
	}
}
