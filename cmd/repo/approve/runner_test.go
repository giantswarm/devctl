package approve

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// The text names the membership, the re-render and the outcome sentence.
func TestPrint(t *testing.T) {
	var out bytes.Buffer
	print(&out, &manager.Approval{
		PullRequest: 6179, Team: "team-bumblebee", Author: "alice", Login: "quentin", Teams: []string{"team-bumblebee", "team-atlas"}, Member: true,
		Rerendered: &manager.Rerendered{Base: "3da626ad", Entries: []string{"bumblebee-slack-round-11"}, Files: []string{"repositories/team-bumblebee.yaml"}},
		ReviewURL:  "https://github.com/giantswarm/github/pull/6179#pullrequestreview-1",
		AutoMerge:  true,
		Message:    "Approved as quentin; giantswarm/github#6179, re-rendered on main first, merges by itself once its checks pass.",
	}, false)
	text := out.String()
	require.Contains(t, text, "pull request #6179 of team-bumblebee: you are a member (quentin, teams team-bumblebee, team-atlas); opened by alice")
	require.Contains(t, text, "re-rendered on 3da626ad first: bumblebee-slack-round-11 in repositories/team-bumblebee.yaml")
	require.Contains(t, text, "review: https://github.com/giantswarm/github/pull/6179#pullrequestreview-1")
	require.Contains(t, text, "Approved as quentin;")
	require.NotContains(t, text, "dry run")

	out.Reset()
	print(&out, &manager.Approval{PullRequest: 6179, Team: "team-bumblebee", Member: true, Merged: true}, true)
	require.Contains(t, out.String(), "dry run: nothing written")
	require.Contains(t, out.String(), "merged\n")
}

// The argument is a pull request number.
func TestPullRequest(t *testing.T) {
	n, err := pullRequest("6179")
	require.NoError(t, err)
	require.Equal(t, 6179, n)
	for _, bad := range []string{"", "x", "0", "-1"} {
		_, err := pullRequest(bad)
		require.True(t, client.IsInvalidFlag(err), bad)
	}
}
