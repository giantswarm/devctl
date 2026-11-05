package releasewait

import (
	"testing"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

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
