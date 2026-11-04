package agentcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestInterrupted(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	timeout := NewExitError(ExitTimeout, VerdictTimeout, "unfinished: build (running)")
	red := NewExitError(ExitRed, VerdictRed, "CI failed")
	refused := NewExitError(ExitRefused, VerdictRefused, "another author")
	tooling := errors.New("invalid configuration")
	auth := NewExitError(ExitAuthRequired, VerdictAuthRequired, "gh has no login")
	for _, tc := range []struct {
		name        string
		ctx         context.Context
		err         error
		interrupted bool
	}{
		{name: "case 0: canceled request", ctx: canceled, err: fmt.Errorf("reading CI: %w", context.Canceled), interrupted: true},
		{name: "case 1: canceled polling wait", ctx: canceled, err: timeout, interrupted: true},
		{name: "case 2: ordinary timeout", ctx: context.Background(), err: timeout},
		{name: "case 3: completed success", ctx: canceled},
		{name: "case 4: completed failure", ctx: canceled, err: red},
		{name: "case 5: completed refusal", ctx: canceled, err: refused},
		{name: "case 6: unrelated tooling failure", ctx: canceled, err: tooling},
		{name: "case 7: canceled login flattened its error", ctx: canceled, err: auth, interrupted: true},
		{name: "case 8: ordinary login refusal", ctx: context.Background(), err: auth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Interrupted(tc.ctx, tc.err)
			if !tc.interrupted {
				if got != tc.err {
					t.Fatalf("completed outcome changed: got %v, want %v", got, tc.err)
				}
				return
			}
			if Exit(got) != ExitTimeout || !strings.Contains(got.Error(), "interrupted") || !strings.Contains(got.Error(), tc.err.Error()) {
				t.Fatalf("interruption lost its state or exit code: %v", got)
			}
		})
	}
}
