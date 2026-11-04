package list_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestListHonorsCanceledContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{"canceled", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}, context.Canceled},
		{"expired deadline", func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Unix(0, 0))
		}, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const cluster = "graveler"
			dir := newCheckout(t, map[string]string{
				"management-clusters/" + cluster + "/configmap-reservations.yaml": "data: {}\n",
			})
			ctx, cancel := tc.ctx()
			defer cancel()

			var stdout bytes.Buffer
			cmd := newCommand(t, &stdout)
			cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster})

			// Sync annotates git errors as syncError, retaining their message rather
			// than their error identity.
			err := cmd.ExecuteContext(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want.Error()) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("canceled list wrote %q", stdout.String())
			}
		})
	}
}
