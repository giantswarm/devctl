package updater

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// githubSource is the releases of one GitHub repository, read with the token
// that Config.GithubToken returns when the first request is made: never when
// the version cache answers, once for a lookup and its download together. An
// empty token reads anonymously. Unlike selfupdate.NewGitHubSource it never
// reads $GITHUB_TOKEN itself, so the caller's token is the only one.
type githubSource struct {
	// repository is the one the downloads come from: a selfupdate.Release
	// does not name its repository to a source.
	repository selfupdate.Repository
	// apiURL is the GitHub REST API; empty is https://api.github.com.
	apiURL string
	token  func(context.Context) (string, error)

	once   sync.Once
	client *github.Client
	err    error
}

func newGitHubSource(repository selfupdate.Repository, apiURL string, token func(context.Context) (string, error)) (*githubSource, error) {
	s := &githubSource{repository: repository, apiURL: apiURL, token: token}
	// A wrong API URL is a wrong Config, told before any request.
	if _, err := s.newClient(""); err != nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitHubAPIURL %q: %v", Config{}, apiURL, err)
	}
	return s, nil
}

// api is the client, built with the token on the first request.
func (s *githubSource) api(ctx context.Context) (*github.Client, error) {
	s.once.Do(func() {
		var token string
		if s.token != nil {
			token, s.err = s.token(ctx)
			if s.err != nil {
				return
			}
		}
		s.client, s.err = s.newClient(token)
	})
	return s.client, s.err
}

// newClient is a client for apiURL, anonymous when token is empty.
func (s *githubSource) newClient(token string) (*github.Client, error) {
	var options []github.ClientOptionsFunc
	if s.apiURL != "" {
		options = append(options, github.WithURLs(&s.apiURL, nil))
	}
	if token != "" {
		options = append(options, github.WithAuthToken(token))
	}
	return github.NewClient(options...)
}

// ListReleases implements selfupdate.Source.
func (s *githubSource) ListReleases(ctx context.Context, repository selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	owner, repo, err := repository.GetSlug()
	if err != nil {
		return nil, err
	}
	client, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	rels, res, err := client.Repositories.ListReleases(ctx, owner, repo, nil)
	if res != nil && res.StatusCode == http.StatusNotFound {
		// No such repository, or not for this token: no release, which
		// the updater reports as such.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	releases := make([]selfupdate.SourceRelease, len(rels))
	for i, rel := range rels {
		releases[i] = githubRelease{rel}
	}
	return releases, nil
}

// DownloadReleaseAsset implements selfupdate.Source.
func (s *githubSource) DownloadReleaseAsset(ctx context.Context, _ *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
	owner, repo, err := s.repository.GetSlug()
	if err != nil {
		return nil, err
	}
	client, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	rc, _, err := client.Repositories.DownloadReleaseAsset(ctx, owner, repo, assetID, http.DefaultClient)
	if err != nil {
		return nil, err
	}
	return rc, nil
}

// githubRelease is a GitHub release as the selfupdate library reads it; its
// assets are go-github's, which read the same.
type githubRelease struct{ *github.RepositoryRelease }

func (r githubRelease) GetPublishedAt() time.Time { return r.RepositoryRelease.GetPublishedAt().Time }
func (r githubRelease) GetReleaseNotes() string   { return r.GetBody() }
func (r githubRelease) GetURL() string            { return r.GetHTMLURL() }

func (r githubRelease) GetAssets() []selfupdate.SourceAsset {
	assets := make([]selfupdate.SourceAsset, len(r.Assets))
	for i, asset := range r.Assets {
		assets[i] = asset
	}
	return assets
}

var (
	_ selfupdate.Source        = (*githubSource)(nil)
	_ selfupdate.SourceRelease = githubRelease{}
)
