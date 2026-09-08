package updater

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"sigs.k8s.io/yaml"
)

// The signature check itself (a bundle that verifies, a tampered binary, a
// bundle for another repository) is tested where it lives, in
// github.com/giantswarm/selfupdate-cosign. What follows checks that
// pkg/updater wires it in so that an unsigned or unverifiable release never
// reaches the disk, and that version lookups stay independent of it.

const repositoryURL = "https://github.com/giantswarm/devctl"

// Versions the tests use: the installed one, a newer release, and a version
// only the cache knows.
const (
	currentVersion = "1.0.0"
	newerVersion   = "2.0.0"
	cachedVersion  = "3.0.0"
)

// fakeSource stands in for GitHub: the releases it lists, the bytes every
// asset download returns, and how often the list was asked for.
type fakeSource struct {
	releases     []selfupdate.SourceRelease
	assets       map[int64][]byte
	listReleases int
}

func (s *fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	s.listReleases++
	return s.releases, nil
}

func (s *fakeSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, id int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.assets[id])), nil
}

type fakeAsset struct {
	id   int64
	name string
}

func (a fakeAsset) GetID() int64                  { return a.id }
func (a fakeAsset) GetName() string               { return a.name }
func (a fakeAsset) GetSize() int                  { return 3 }
func (a fakeAsset) GetBrowserDownloadURL() string { return "https://example.test/" + a.name }

type fakeRelease struct {
	tag    string
	assets []selfupdate.SourceAsset
}

func (r fakeRelease) GetID() int64              { return 1 }
func (r fakeRelease) GetTagName() string        { return r.tag }
func (r fakeRelease) GetDraft() bool            { return false }
func (r fakeRelease) GetPrerelease() bool       { return false }
func (r fakeRelease) GetPublishedAt() time.Time { return time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC) }
func (r fakeRelease) GetReleaseNotes() string   { return "notes" }
func (r fakeRelease) GetName() string           { return r.tag }
func (r fakeRelease) GetURL() string {
	return repositoryURL + "/releases/tag/" + r.tag
}
func (r fakeRelease) GetAssets() []selfupdate.SourceAsset { return r.assets }

// binaryAsset is the asset name architect publishes for this platform.
func binaryAsset() string {
	name := "devctl-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Asset IDs the fakes use: 1 is this platform's binary, 2 its bundle.
const (
	binaryAssetID = 1
	bundleAssetID = 2
)

// unsignedRelease is a release that carries this platform's binary but no
// signature bundle next to it.
func unsignedRelease(tag string) fakeRelease {
	return fakeRelease{tag: tag, assets: []selfupdate.SourceAsset{
		fakeAsset{binaryAssetID, binaryAsset()},
	}}
}

// signedRelease is a release that carries this platform's binary and a
// bundle next to it (whether the bundle verifies is up to the download).
func signedRelease(tag string) fakeRelease {
	return fakeRelease{tag: tag, assets: []selfupdate.SourceAsset{
		fakeAsset{binaryAssetID, binaryAsset()},
		fakeAsset{bundleAssetID, binaryAsset() + ".bundle"},
	}}
}

// newUpdater points the package at src instead of GitHub and returns an
// Updater for currentVersion of giantswarm/devctl.
func newUpdater(t *testing.T, src *fakeSource, cacheDir string) *Updater {
	t.Helper()
	prev := releaseSource
	releaseSource = src
	t.Cleanup(func() { releaseSource = prev })

	u, err := New(Config{
		CurrentVersion: currentVersion,
		RepositoryURL:  repositoryURL,
		CacheDir:       cacheDir,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return u
}

// installFixture makes InstallLatest replace a throwaway file instead of the
// running executable; it returns that file's path and its content, so a test
// can assert the file survived.
func installFixture(t *testing.T) (string, []byte) {
	t.Helper()
	installed := []byte("the devctl that is installed right now")
	exe := filepath.Join(t.TempDir(), "devctl")
	if err := os.WriteFile(exe, installed, 0o755); err != nil { //nolint:gosec // an executable
		t.Fatal(err)
	}
	prev := executablePath
	executablePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executablePath = prev })
	return exe, installed
}

func assertUnchanged(t *testing.T, exe string, installed []byte) {
	t.Helper()
	got, err := os.ReadFile(exe) //nolint:gosec // the test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, installed) {
		t.Fatalf("the installed binary was replaced: %q", got)
	}
}

func writeCache(t *testing.T, dir string, c cache) {
	t.Helper()
	serialized, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cacheFileName), serialized, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsAnIncompleteConfig(t *testing.T) {
	if _, err := New(Config{RepositoryURL: repositoryURL}); !IsInvalidConfig(err) {
		t.Errorf("an empty CurrentVersion should be an invalid config, got: %v", err)
	}
	if _, err := New(Config{CurrentVersion: currentVersion}); !IsInvalidConfig(err) {
		t.Errorf("an empty RepositoryURL should be an invalid config, got: %v", err)
	}
}

func TestGetLatestReportsANewerRelease(t *testing.T) {
	src := &fakeSource{releases: []selfupdate.SourceRelease{signedRelease("v" + newerVersion)}}
	u := newUpdater(t, src, "")

	version, err := u.GetLatest()
	if !IsHasNewVersion(err) {
		t.Fatalf("a newer release should be reported as hasNewVersion, got: %v", err)
	}
	if version != newerVersion {
		t.Errorf("version = %q, want %s", version, newerVersion)
	}
}

func TestGetLatestPicksTheHighestVersionOfManyReleases(t *testing.T) {
	src := &fakeSource{releases: []selfupdate.SourceRelease{
		signedRelease("v" + newerVersion), signedRelease("v3.1.0"), signedRelease("v3.0.9"),
	}}
	u := newUpdater(t, src, "")

	version, err := u.GetLatest()
	if !IsHasNewVersion(err) {
		t.Fatalf("a newer release should be reported as hasNewVersion, got: %v", err)
	}
	if version != "3.1.0" {
		t.Errorf("version = %q, want 3.1.0", version)
	}
}

func TestGetLatestIsNilWhenAlreadyCurrent(t *testing.T) {
	src := &fakeSource{releases: []selfupdate.SourceRelease{signedRelease("v" + currentVersion)}}
	u := newUpdater(t, src, "")

	version, err := u.GetLatest()
	if err != nil {
		t.Fatalf("the current version should not be an error, got: %v", err)
	}
	if version != currentVersion {
		t.Errorf("version = %q, want %s", version, currentVersion)
	}
}

func TestGetLatestReportsWhenThereIsNoRelease(t *testing.T) {
	u := newUpdater(t, &fakeSource{}, "")

	if _, err := u.GetLatest(); !IsVersionNotFound(err) {
		t.Errorf("no release should be versionNotFound, got: %v", err)
	}

	// A release that has no binary for this platform counts as none, too.
	src := &fakeSource{releases: []selfupdate.SourceRelease{
		fakeRelease{tag: "v" + newerVersion, assets: []selfupdate.SourceAsset{fakeAsset{9, "devctl-plan9-mips"}}},
	}}
	u = newUpdater(t, src, "")

	if _, err := u.GetLatest(); !IsVersionNotFound(err) {
		t.Errorf("no asset for this platform should be versionNotFound, got: %v", err)
	}
}

// A version lookup must never depend on a release being signed: the check
// that runs before every devctl command must not break when a release lacks
// its bundle.
func TestGetLatestDoesNotNeedASignatureBundle(t *testing.T) {
	src := &fakeSource{releases: []selfupdate.SourceRelease{unsignedRelease("v" + newerVersion)}}
	u := newUpdater(t, src, "")

	version, err := u.GetLatest()
	if !IsHasNewVersion(err) {
		t.Fatalf("an unsigned newer release should still be reported, got: %v", err)
	}
	if version != newerVersion {
		t.Errorf("version = %q, want %s", version, newerVersion)
	}
}

func TestGetLatestUsesAFreshCacheWithoutAskingTheSource(t *testing.T) {
	dir := t.TempDir()
	writeCache(t, dir, cache{LastUpdate: time.Now().UTC(), LatestVersion: cachedVersion})
	src := &fakeSource{releases: []selfupdate.SourceRelease{signedRelease("v" + newerVersion)}}
	u := newUpdater(t, src, dir)

	version, err := u.GetLatest()
	if !IsHasNewVersion(err) {
		t.Fatalf("the cached newer version should be reported, got: %v", err)
	}
	if version != cachedVersion {
		t.Errorf("version = %q, want the cached %s", version, cachedVersion)
	}
	if src.listReleases != 0 {
		t.Errorf("the source was asked %d time(s) although the cache is fresh", src.listReleases)
	}
}

func TestGetLatestIgnoresAnExpiredCache(t *testing.T) {
	dir := t.TempDir()
	writeCache(t, dir, cache{LastUpdate: time.Now().UTC().Add(-cacheValidationDuration - time.Minute), LatestVersion: cachedVersion})
	src := &fakeSource{releases: []selfupdate.SourceRelease{signedRelease("v" + newerVersion)}}
	u := newUpdater(t, src, dir)

	version, err := u.GetLatest()
	if !IsHasNewVersion(err) {
		t.Fatalf("the newer release should be reported, got: %v", err)
	}
	if version != newerVersion {
		t.Errorf("version = %q, want %s from the source, not the stale cache", version, newerVersion)
	}
	if src.listReleases != 1 {
		t.Errorf("the source was asked %d time(s), want 1", src.listReleases)
	}
}

func TestGetLatestPersistsTheCacheForTheNextUpdater(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{releases: []selfupdate.SourceRelease{signedRelease("v" + newerVersion)}}

	if _, err := newUpdater(t, src, dir).GetLatest(); !IsHasNewVersion(err) {
		t.Fatalf("the newer release should be reported, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cacheFileName)); err != nil {
		t.Fatalf("the cache file should have been written: %v", err)
	}

	// A new Updater restores the cache in New and does not ask the source.
	version, err := newUpdater(t, src, dir).GetLatest()
	if !IsHasNewVersion(err) {
		t.Fatalf("the cached newer version should be reported, got: %v", err)
	}
	if version != newerVersion {
		t.Errorf("version = %q, want %s", version, newerVersion)
	}
	if src.listReleases != 1 {
		t.Errorf("the source was asked %d time(s) across two updaters, want 1", src.listReleases)
	}
}

func TestInstallLatestDoesNothingWhenAlreadyCurrent(t *testing.T) {
	src := &fakeSource{
		releases: []selfupdate.SourceRelease{signedRelease("v" + currentVersion)},
		assets:   map[int64][]byte{binaryAssetID: []byte("the same devctl"), bundleAssetID: []byte("{}")},
	}
	u := newUpdater(t, src, "")
	exe, installed := installFixture(t)

	if err := u.InstallLatest(); err != nil {
		t.Fatalf("nothing to install should not be an error, got: %v", err)
	}
	assertUnchanged(t, exe, installed)
}

func TestInstallLatestRefusesAReleaseWithoutASignatureBundle(t *testing.T) {
	src := &fakeSource{
		releases: []selfupdate.SourceRelease{unsignedRelease("v" + newerVersion)},
		assets:   map[int64][]byte{binaryAssetID: []byte("a newer devctl, unsigned")},
	}
	u := newUpdater(t, src, "")
	exe, installed := installFixture(t)

	err := u.InstallLatest()
	if err == nil {
		t.Fatal("a release without a bundle must be refused")
	}
	if !strings.Contains(err.Error(), "no signature bundle") {
		t.Errorf("the error should say what is missing, got: %v", err)
	}
	assertUnchanged(t, exe, installed)
}

func TestInstallLatestRefusesADownloadThatDoesNotVerify(t *testing.T) {
	src := &fakeSource{
		releases: []selfupdate.SourceRelease{signedRelease("v" + newerVersion)},
		assets: map[int64][]byte{
			binaryAssetID: []byte("a newer devctl"),
			bundleAssetID: []byte("{}"), // not a Sigstore bundle
		},
	}
	u := newUpdater(t, src, "")
	exe, installed := installFixture(t)

	err := u.InstallLatest()
	if err == nil {
		t.Fatal("a download whose bundle does not verify must be refused")
	}
	if !strings.Contains(err.Error(), "is unchanged") || !strings.Contains(err.Error(), "is not a Sigstore bundle") {
		t.Errorf("the error should say the binary was refused and why, got: %v", err)
	}
	assertUnchanged(t, exe, installed)
}
