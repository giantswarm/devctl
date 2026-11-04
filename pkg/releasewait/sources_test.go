package releasewait

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestNewSourcesSeparatesTeamFileCredentials(t *testing.T) {
	for _, separate := range []bool{false, true} {
		t.Run(fmt.Sprint(separate), func(t *testing.T) {
			team := "- name: app\n  gen:\n    flavours: [app]\n    language: go\n    ci:\n      generate: true\n"
			gh, err := githubmock.Start(sequence.Routes{
				"GET /repos/giantswarm/app":                          {{Body: map[string]any{"name": "app"}}},
				"GET /repos/giantswarm/github/contents/repositories": {{Body: []map[string]any{{"name": "team-test.yaml", "path": "repositories/team-test.yaml", "type": "file"}}}},
				"GET /repos/giantswarm/github/contents/repositories/team-test.yaml": {{Body: map[string]any{
					"type": "file", "encoding": "base64", "path": "repositories/team-test.yaml", "content": base64.StdEncoding.EncodeToString([]byte(team)),
				}}},
			})
			require.NoError(t, err)
			t.Cleanup(gh.Close)
			credentials := Credentials{GitHubToken: "ghu_repository_fixture"}
			if separate {
				credentials.TeamGitHubToken = "ghu_team_fixture"
			}
			sources, err := NewSources(agentcli.Endpoints{GitHubAPIURL: gh.URL}, nil, credentials)
			require.NoError(t, err)
			_, found, err := sources.Entries.FindEntry(t.Context(), "giantswarm", "app")
			require.NoError(t, err)
			require.True(t, found)
			_, err = sources.GitHub.IsPrivateRepository(t.Context(), "giantswarm", "app")
			require.NoError(t, err)
			require.Len(t, gh.Requests(), 3)
			for _, request := range gh.Requests() {
				want := "Bearer ghu_repository_fixture"
				if separate && strings.HasPrefix(request.Path, "/repos/giantswarm/github/") {
					want = "Bearer ghu_team_fixture"
				}
				require.Equal(t, want, request.Header.Get("Authorization"), request.Path)
				require.Equal(t, "GET", request.Method)
			}
		})
	}
}

// TestNewSourcesActsWithTheHandedInToken builds a wait's sources from the
// token a server holds for a person: every GitHub request carries it.
func TestNewSourcesActsWithTheHandedInToken(t *testing.T) {
	gh, err := githubmock.Start(sequence.Routes{
		"GET /repos/giantswarm/klaus": {{Body: map[string]any{"name": "klaus", "default_branch": "main"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gh.Close)

	sources, err := NewSources(agentcli.Endpoints{GitHubAPIURL: gh.URL}, nil, Credentials{GitHubToken: "ghu_person"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sources.Client.GitHub().Repositories.Get(t.Context(), "giantswarm", "klaus"); err != nil {
		t.Fatal(err)
	}
	requests := gh.Requests()
	if len(requests) != 1 || requests[0].Header.Get("Authorization") != "Bearer ghu_person" {
		t.Errorf("want one request with the person's token, got %+v", requests)
	}
}

// TestNewSourcesWithoutACircleCITokenIsExit8: a caller that hands in no
// CircleCI token (a server holds none) gets exit 8 when a tag's pipeline
// runs on CircleCI.
func TestNewSourcesWithoutACircleCITokenIsExit8(t *testing.T) {
	sources, err := NewSources(agentcli.Endpoints{GitHubAPIURL: "http://127.0.0.1:1"}, nil, Credentials{GitHubToken: "ghu_person"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sources.CircleCI(t.Context()); agentcli.Exit(err) != agentcli.ExitAuthRequired {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitAuthRequired, agentcli.Exit(err), err)
	}
}
