package reposetup

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/stretchr/testify/require"
)

type fakeRepositories struct {
	repo *github.Repository
	err  error
}

func (f fakeRepositories) GetRepository(context.Context, string, string) (*github.Repository, error) {
	return f.repo, f.err
}

func TestGitHubNameChecker(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		repos   fakeRepositories
		verdict Verdict
		detail  string
		wantErr bool
	}{
		{
			name:    "404 from go-github is free",
			repos:   fakeRepositories{err: &github.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}}},
			verdict: VerdictFree,
			detail:  "no repository giantswarm/new-repo on GitHub",
		},
		{
			name:    "existing repository is taken",
			repos:   fakeRepositories{repo: &github.Repository{FullName: new("giantswarm/new-repo")}},
			verdict: VerdictTaken,
			detail:  "repository giantswarm/new-repo exists",
		},
		{
			name:    "archived repository is taken",
			repos:   fakeRepositories{repo: &github.Repository{FullName: new("giantswarm/new-repo"), Archived: new(true)}},
			verdict: VerdictTaken,
			detail:  "repository giantswarm/new-repo exists (archived)",
		},
		{
			name:    "redirect to a renamed repository is taken",
			repos:   fakeRepositories{repo: &github.Repository{FullName: new("giantswarm/renamed-repo")}},
			verdict: VerdictTaken,
			detail:  "giantswarm/new-repo redirects to giantswarm/renamed-repo: the name belongs to a renamed repository",
		},
		{
			name:    "case differs only is the same repository",
			repos:   fakeRepositories{repo: &github.Repository{FullName: new("GiantSwarm/New-Repo")}},
			verdict: VerdictTaken,
			detail:  "repository giantswarm/new-repo exists",
		},
		{
			name:    "any other error is an error",
			repos:   fakeRepositories{err: errors.New("boom")},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			check, err := GitHubNameChecker{Repositories: tc.repos}.CheckName(ctx, "giantswarm", "new-repo")
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, NameCheck{Verdict: tc.verdict, Detail: tc.detail}, check)
		})
	}

	t.Run("no repositories client", func(t *testing.T) {
		_, err := GitHubNameChecker{}.CheckName(ctx, "giantswarm", "new-repo")
		require.True(t, IsInvalidConfig(err))
	})
}
