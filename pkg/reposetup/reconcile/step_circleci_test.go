package reconcile

import (
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/stretchr/testify/require"
)

// TestIsReleaseTag: the release step verifies a vX.Y.Z tag, a pre-release
// suffix allowed — what auto-release cuts and the generated pipeline's tag
// filter builds — and nothing tagged otherwise.
func TestIsReleaseTag(t *testing.T) {
	cases := map[string]bool{
		"v0.1.0":         true,
		"v10.20.30":      true,
		"v1.2.3-rc.1":    true,
		"v1.2.3-dev.abc": true,
		"0.1.0":          false, // auto-release cuts the v
		"v0.1":           false, // not a full version
		"v01.2.3":        false, // a leading zero is no semver
		"base/v0.1.0":    false, // a per-component tag
		"release-1.2.3":  false,
		"":               false,
	}
	for tag, want := range cases {
		t.Run(tag, func(t *testing.T) {
			require.Equal(t, want, isReleaseTag(tag))
		})
	}
}

// TestHasCircleCIWebhook: CircleCI's webhook counts when it is active and
// subscribed to push events; another URL, an inactive hook or one without
// push does not.
func TestHasCircleCIWebhook(t *testing.T) {
	hook := func(url string, active bool, events ...string) *github.Hook {
		return &github.Hook{Active: new(active), Events: events, Config: &github.HookConfig{URL: new(url)}}
	}
	require.False(t, HasCircleCIWebhook(nil))
	require.False(t, HasCircleCIWebhook([]*github.Hook{
		hook("https://circleci.com/hooks/github", false, "push"),
		hook("https://circleci.com/hooks/github", true, "pull_request"),
		hook("https://github-pr-webhook.ci.giantswarm.io", true, "push"),
	}))
	require.True(t, HasCircleCIWebhook([]*github.Hook{
		hook("https://github-pr-webhook.ci.giantswarm.io", true, "push"),
		hook("https://circleci.com/hooks/github", true, "fork", "pull_request", "push"),
	}))
}
