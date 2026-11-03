package updater

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/giantswarm/go-selfupdate"
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// GitHub intermittently answers the release list with 200 and no releases at
// all: an empty list is asked for again this many times, waiting
// listRetryDelay times the attempt in between.
var (
	listRetries    = 2
	listRetryDelay = 500 * time.Millisecond
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

// ListReleases implements selfupdate.Source. GitHub intermittently answers
// the release list with 200 and no releases at all, independently each time:
// an empty list is asked for again. The latest release, answered reliably, is
// asked for on its own and added when the list lacks it, so a list that stays
// empty still finds it; it is the most recently published release, not
// necessarily the highest version, hence the list's second chance. A failed
// latest lookup is an error only when the list is empty too.
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
	for retry := 1; err == nil && len(rels) == 0 && retry <= listRetries; retry++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(retry) * listRetryDelay):
		}
		rels, res, err = client.Repositories.ListReleases(ctx, owner, repo, nil)
	}
	if res != nil && res.StatusCode == http.StatusNotFound {
		// No such repository, or not for this token: no release, which
		// the updater reports as such.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	releases := make([]selfupdate.SourceRelease, 0, len(rels)+1)
	for _, rel := range rels {
		releases = append(releases, githubRelease{rel})
	}

	latest, res, err := client.Repositories.GetLatestRelease(ctx, owner, repo)
	switch {
	case res != nil && res.StatusCode == http.StatusNotFound:
		// No release yet.
		return releases, nil
	case err != nil && len(releases) == 0:
		return nil, err
	case err != nil:
		return releases, nil
	}
	for _, rel := range rels {
		if rel.GetID() == latest.GetID() {
			return releases, nil
		}
	}
	return append(releases, githubRelease{latest}), nil
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
