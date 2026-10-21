package releasepromote

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// TestNewSourcesActsWithTheHandedInToken builds a promotion's sources from
// the token a server holds for a person: every request carries it, and the
// document's identity and 404 hint are the caller's.
func TestNewSourcesActsWithTheHandedInToken(t *testing.T) {
	teamFile := "- name: klaus\n  gen:\n    flavours: [app]\n    language: go\n    ci:\n      generate: true\n"
	gh, err := githubmock.Start(sequence.Routes{
		"GET /repos/giantswarm/github/contents/repositories/team-bumblebee.yaml": {{Body: map[string]any{
			"type": "file", "encoding": "base64", "path": "repositories/team-bumblebee.yaml", "name": "team-bumblebee.yaml", "sha": "abc",
			"content": base64.StdEncoding.EncodeToString([]byte(teamFile)),
		}}},
	})
	require.NoError(t, err)
	t.Cleanup(gh.Close)

	identity := Identity{Source: "server", Login: "a-person"}
	sources, err := NewSources(agentcli.Endpoints{GitHubAPIURL: gh.URL}, nil, Credentials{GitHubToken: "ghu_person", Identity: identity, NotFoundHint: "the hint"})
	require.NoError(t, err)
	require.Equal(t, identity, sources.Identity)
	require.Equal(t, "the hint", sources.NotFoundHint)

	repositories, err := sources.Team(t.Context(), "team-bumblebee")
	require.NoError(t, err)
	require.Equal(t, []string{"giantswarm/klaus"}, repositories)
	requests := gh.Requests()
	require.NotEmpty(t, requests)
	for _, r := range requests {
		require.Equal(t, "Bearer ghu_person", r.Header.Get("Authorization"), r.String())
	}
}
