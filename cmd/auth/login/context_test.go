package login

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestRunHonorsCommandCancellation(t *testing.T) {
	cases := []struct {
		name      string
		cancelNow bool
	}{
		{name: "case 0: canceled before login", cancelNow: true},
		{name: "case 1: canceled while requesting browser consent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelNow {
				cancel()
			}
			r, store, stdout, stderr := newRunnerWithBrowser(t, fakeGitHub(t), func(string) error {
				cancel()
				return nil
			})
			r.flag.GitHubOnly = true
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			err := r.Run(cmd, nil)
			if agentcli.Exit(err) != agentcli.ExitUsage {
				t.Fatalf("exit = %d, want 7 after command cancellation", agentcli.Exit(err))
			}
			if _, err := store.Get(authstore.UserGitHub); !errors.Is(err, authstore.ErrNotFound) {
				t.Fatalf("a canceled login stored a GitHub identity: %v", err)
			}
			var doc document
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("stdout is not one JSON document: %v", err)
			}
			if doc.ExitCode != agentcli.ExitUsage || doc.Verdict != agentcli.VerdictUsage || !strings.Contains(doc.Reason, context.Canceled.Error()) || doc.GitHub.Present {
				t.Fatalf("cancellation document = %+v", doc)
			}
			for _, token := range []string{"ghu_secret", "ghr_secret"} {
				if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
					t.Fatal("output contains a synthetic token")
				}
			}
		})
	}
}
