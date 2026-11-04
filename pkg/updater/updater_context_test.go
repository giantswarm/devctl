package updater

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/go-selfupdate"
)

type contextSource struct {
	fakeSource
	list func(context.Context) ([]selfupdate.SourceRelease, error)
}

func (s *contextSource) ListReleases(ctx context.Context, _ selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return s.list(ctx)
}

// The caller's context must reach the release source. Even if the source
// returns a usable release after cancellation, it must not become a successful
// check or replace the existing cache.
func TestGetLatestFromSourceContextCancelsWithoutRefreshingTheCache(t *testing.T) {
	for _, returnRelease := range []bool{false, true} {
		name := "case 0: source returns cancellation"
		if returnRelease {
			name = "case 1: source returns releases after cancellation"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeCache(t, dir, cache{LastUpdate: time.Now().UTC(), LatestVersion: cachedVersion})
			path := filepath.Join(dir, cacheFileName)
			before, err := os.ReadFile(path) // #nosec G304 -- fixture path under t.TempDir()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			called := false
			src := &contextSource{list: func(got context.Context) ([]selfupdate.SourceRelease, error) {
				called = true
				if got != ctx {
					t.Error("the release source did not receive the caller's context")
				}
				cancel()
				if returnRelease {
					return []selfupdate.SourceRelease{unsignedRelease("v" + newerVersion)}, nil
				}
				return nil, ctx.Err()
			}}
			prev := releaseSource
			releaseSource = src
			t.Cleanup(func() { releaseSource = prev })
			u, err := New(Config{CurrentVersion: currentVersion, RepositoryURL: repositoryURL, CacheDir: dir})
			if err != nil {
				t.Fatal(err)
			}

			version, err := u.GetLatestFromSourceContext(ctx)
			if !errors.Is(err, context.Canceled) || version != "" || !called {
				t.Errorf("lookup = %q, %v (source called %v); want cancellation", version, err, called)
			}
			after, err := os.ReadFile(path) // #nosec G304 -- fixture path under t.TempDir()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("canceled lookup replaced the cache: %q", after)
			}
		})
	}
}

func TestGetLatestFromSourceContextAlreadyCanceledDoesNotAskTheSource(t *testing.T) {
	src := &fakeSource{releases: []selfupdate.SourceRelease{unsignedRelease("v" + newerVersion)}}
	u := newUpdater(t, src, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := u.GetLatestFromSourceContext(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("lookup: %v, want cancellation", err)
	}
	if src.listReleases != 0 {
		t.Errorf("canceled lookup asked the source %d times", src.listReleases)
	}
}

func TestGetLatestFromSourceContextReachesTokenResolution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	u, err := New(Config{
		CurrentVersion: currentVersion,
		RepositoryURL:  repositoryURL,
		GitHubAPIURL:   "http://127.0.0.1:1",
		GithubToken: func(got context.Context) (string, error) {
			called = true
			if got != ctx {
				t.Error("token resolution did not receive the caller's context")
			}
			cancel()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.GetLatestFromSourceContext(ctx); !errors.Is(err, context.Canceled) || !called {
		t.Errorf("lookup: %v (token requested %v), want cancellation from token resolution", err, called)
	}
}
