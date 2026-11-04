package updater

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/blang/semver"
	"github.com/giantswarm/go-selfupdate"
	"github.com/giantswarm/microerror"
	selfupdatecosign "github.com/giantswarm/selfupdate-cosign"
)

type Config struct {
	// GithubToken returns the GitHub token the releases are read with. It is
	// called once, when the first request goes to GitHub, and never when the
	// version cache answers. nil or "" reads anonymously; the environment is
	// not a source of its own.
	GithubToken func(context.Context) (string, error)
	// GitHubAPIURL is the GitHub REST API; empty is https://api.github.com.
	GitHubAPIURL string
	// CurrentVersion is the currently installed version
	// of the application.
	CurrentVersion string
	// RepositoryURL is the URL to the GitHub repository.
	RepositoryURL string
	// CacheDir is the path to the directory where the
	// cache should be stored.
	CacheDir string
}

// Seams for the tests: where releases come from (nil is GitHub, reached with
// Config.GithubToken at Config.GitHubAPIURL), which file InstallLatest
// replaces (the running executable) and what a download must verify against
// before it is installed (its cosign Sigstore bundle).
var (
	releaseSource      selfupdate.Source
	executablePath     = selfupdate.ExecutablePath
	signatureValidator = func(repository string) selfupdate.Validator { return selfupdatecosign.New(repository) }
)

type Updater struct {
	currentVersion semver.Version
	repository     string
	cacheDir       string

	// lookup finds the latest release without looking for signature
	// bundles, so a version lookup (`version check`, and the check that
	// runs before every command) never depends on a release being signed.
	lookup *selfupdate.Updater
	// installer finds the latest release together with the cosign Sigstore
	// bundle published next to this platform's binary and verifies the
	// download against it before anything is written.
	installer *selfupdate.Updater
	cache     *cache
}

func New(c Config) (*Updater, error) {
	if len(c.CurrentVersion) < 1 {
		return nil, microerror.Maskf(invalidConfigError, "%T.CurrentVersion must not be empty", c)
	}
	if len(c.RepositoryURL) < 1 {
		return nil, microerror.Maskf(invalidConfigError, "%T.RepositoryURL must not be empty", c)
	}

	var err error

	u := &Updater{
		cacheDir: c.CacheDir,
	}

	{
		u.repository, err = u.parseRepoFromURL(c.RepositoryURL)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	{
		u.currentVersion, err = semver.Parse(c.CurrentVersion)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	{
		source := releaseSource
		if source == nil {
			source, err = newGitHubSource(selfupdate.ParseSlug(u.repository), c.GitHubAPIURL, c.GithubToken)
			if err != nil {
				return nil, microerror.Mask(err)
			}
		}

		u.lookup, err = selfupdate.NewUpdater(selfupdate.Config{
			Source: source,
		})
		if err != nil {
			return nil, microerror.Mask(err)
		}

		u.installer, err = selfupdate.NewUpdater(selfupdate.Config{
			Source:    source,
			Validator: signatureValidator(u.repository),
		})
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	{
		u.cache = &cache{}
		if len(u.cacheDir) > 0 {
			// We ignore the error on purpose, since we'll just override the config file
			// if it's invalid.
			_ = u.cache.Restore(u.cacheDir)
		}
	}

	return u, nil
}

// InstallLatest installs the newest version that can be installed: the
// latest release's binary for this platform, once it verifies against the
// cosign Sigstore bundle published next to it. A release without a bundle is
// refused before anything is downloaded, a download that does not match its
// signature before anything is written; the installed binary stays as it is
// either way. The new binary replaces the running executable, symbolic links
// resolved, with a single rename: a devctl started meanwhile runs the old
// binary or the new one, several updates may run at once, and no other file
// is touched.
func (u *Updater) InstallLatest() error {
	_, err := u.InstallLatestWithResult()
	return err
}

// InstallResult reports the release selected for installation and whether it
// actually replaced the executable. A release at or below the running version
// is a successful no-op, with Installed false.
type InstallResult struct {
	Version   string
	Installed bool
}

// InstallLatestWithResult installs with the same verification guarantees as
// InstallLatest and reports the actual outcome. The version cache is refreshed
// from this selection after a successful install or no-op, since discovery may
// have seen a different release before installation.
func (u *Updater) InstallLatestWithResult() (InstallResult, error) {
	ctx := context.Background()

	// The release must come from the validating updater so that the bundle
	// asset is attached to it.
	latest, found, err := u.installer.DetectLatest(ctx, selfupdate.ParseSlug(u.repository))
	if errors.Is(err, selfupdate.ErrValidationAssetNotFound) {
		return InstallResult{}, microerror.Mask(fmt.Errorf("the latest release of %s has no signature bundle for this platform's binary, so it cannot be verified; refusing to install it: %w", u.repository, err))
	} else if err != nil {
		return InstallResult{}, microerror.Mask(err)
	}

	if !found {
		return InstallResult{}, microerror.Maskf(versionNotFoundError, "couldn't find the latest version and/or release assets on GitHub, probably due to token without access to the repository %s.", u.repository)
	}

	latestVersion, err := semver.Parse(latest.Version())
	if err != nil {
		return InstallResult{}, microerror.Mask(err)
	}
	result := InstallResult{Version: latestVersion.String()}

	if latestVersion.LTE(u.currentVersion) {
		// Nothing newer to install.
		u.persistLatestVersion(latestVersion)
		return result, nil
	}

	exe, err := executablePath()
	if err != nil {
		return InstallResult{}, microerror.Mask(err)
	}

	err = selfupdatecosign.Install(ctx, u.installer, latest, exe)
	if err != nil {
		return InstallResult{}, microerror.Mask(fmt.Errorf("update failed, %s is unchanged: %w", exe, err))
	}

	result.Installed = true
	u.persistLatestVersion(latestVersion)
	return result, nil
}

// GetLatest returns the latest version available in the
// source repository, and if we can upgrade to that version
// or not (it can be equal to the current version). A cache younger than an
// hour answers instead of the source: the check that runs before every
// command.
func (u *Updater) GetLatest() (version string, err error) {
	return u.latest(context.Background(), true)
}

// GetLatestFromSource is GetLatest asking the source whatever the cache
// says, and refreshing the cache with the answer: an explicit `version
// update` or `version check` right after a release finds it, where the
// cache written before the release would deny it for up to an hour.
func (u *Updater) GetLatestFromSource() (version string, err error) {
	return u.GetLatestFromSourceContext(context.Background())
}

// GetLatestFromSourceContext is GetLatestFromSource with the caller's context
// governing token resolution and release requests. A canceled lookup does not
// refresh the cache.
func (u *Updater) GetLatestFromSourceContext(ctx context.Context) (version string, err error) {
	return u.latest(ctx, false)
}

func (u *Updater) latest(ctx context.Context, readCache bool) (version string, err error) {
	latestVersion, err := u.getLatestVersion(ctx, readCache)
	if err != nil {
		return "", microerror.Mask(err)
	}

	if latestVersion.GT(u.currentVersion) {
		err = microerror.Maskf(hasNewVersionError, "Version %s available", latestVersion.String())
	} else {
		err = nil
	}

	return latestVersion.String(), err
}

func (u *Updater) getLatestVersion(ctx context.Context, readCache bool) (semver.Version, error) {
	if err := ctx.Err(); err != nil {
		return semver.Version{}, microerror.Mask(err)
	}

	allowCache := len(u.cacheDir) > 0

	if allowCache && readCache && !u.cache.IsExpired() {
		version, err := semver.Parse(u.cache.LatestVersion)
		// If this not a valid semver version, then it means
		// that the someone fiddled with the cache file. We'll
		// just override it.
		if err == nil {
			return version, nil
		}
	}

	latest, found, err := u.lookup.DetectLatest(ctx, selfupdate.ParseSlug(u.repository))
	if err != nil {
		return semver.Version{}, microerror.Mask(err)
	}
	if err := ctx.Err(); err != nil {
		return semver.Version{}, microerror.Mask(err)
	}

	if !found {
		return semver.Version{}, microerror.Maskf(versionNotFoundError, "couldn't find the latest version and/or release assets on GitHub, probably due to token without access to the repository %s.", u.repository)
	}

	latestVersion, err := semver.Parse(latest.Version())
	if err != nil {
		return semver.Version{}, microerror.Mask(err)
	}

	u.persistLatestVersion(latestVersion)

	return latestVersion, nil
}

func (u *Updater) persistLatestVersion(latestVersion semver.Version) {
	if len(u.cacheDir) > 0 {
		u.cache.LatestVersion = latestVersion.String()

		err := u.cache.Persist(u.cacheDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to persist the cache with error: %s", err)
		}
	}
}

func (u *Updater) parseRepoFromURL(sourceURL string) (string, error) {
	repo, err := url.Parse(sourceURL)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return strings.TrimPrefix(repo.Path, "/"), nil
}
