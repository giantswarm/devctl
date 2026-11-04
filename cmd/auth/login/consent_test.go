package login

import (
	"context"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

func TestSignInManagerConsentURL(t *testing.T) {
	const consentURL = "https://consent.example.invalid/authorize?client_id=fixture&redirect_uri=https%3A%2F%2Fmuster.example.invalid%2Fcallback&state=fixture"
	cases := []struct {
		name   string
		answer any
	}{
		{
			name:   "case 0: structured challenge",
			answer: map[string]any{"authUrl": consentURL, "clientIdMethod": "cimd"},
		},
		{
			name:   "case 1: legacy text challenge",
			answer: "Please sign in:\n\n" + consentURL + "\n\nAfter signing in, run this tool again.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := muster.Start(muster.Config{
				Bearer: "fixture-token",
				Tools: map[string][]muster.ToolResponse{
					manager.ToolAuthLogin: {{Result: tc.answer, Args: map[string]any{"server": manager.Server}}},
					manager.ToolGetInfo:   {{Result: map[string]any{"caller": map[string]any{"login": "octocat"}}}},
				},
			})
			defer m.Close()
			var opened []string
			r, _, _, stderr := newRunnerWithBrowser(t, nil, func(url string) error {
				opened = append(opened, url)
				return nil
			})
			signIn, err := r.signInManager(context.Background(), authstore.Token{Endpoint: m.MCPURL(), Value: "fixture-token"}, agentcli.NewProgress(stderr, false))
			if err != nil {
				t.Fatal(err)
			}
			if len(opened) != 1 || opened[0] != consentURL {
				t.Fatalf("browser URLs = %q, want exactly %q", opened, consentURL)
			}
			if !strings.Contains(stderr.String(), "open "+consentURL+" and authorize") {
				t.Fatalf("stderr lacks the original consent URL: %s", stderr.String())
			}
			if !signIn.SignedIn || signIn.Caller != "octocat" {
				t.Fatalf("manager sign-in = %+v", signIn)
			}
		})
	}
}
