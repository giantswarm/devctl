package updater

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/blang/semver"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/giantswarm/microerror"
	selfupdatecosign "github.com/giantswarm/selfupdate-cosign"
)

type Config struct {
	// GithubToken will be used when fetching versions
	// from private GitHub repositories.
	GithubToken string
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
// Config.GithubToken) and which file InstallLatest replaces (the running
// executable).
var (
	releaseSource  selfupdate.Source
	executablePath = selfupdate.ExecutablePath
)

type Updater struct {
	githubToken    string
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
		githubToken: c.GithubToken,
		cacheDir:    c.CacheDir,
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
			// With an empty token the library falls back to GITHUB_TOKEN.
			source, err = selfupdate.NewGitHubSource(selfupdate.GitHubConfig{
				APIToken: u.githubToken,
			})
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
			Validator: selfupdatecosign.New(u.repository),
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
// either way.
func (u *Updater) InstallLatest() error {
	ctx := context.Background()

	// The release must come from the validating updater so that the bundle
	// asset is attached to it.
	latest, found, err := u.installer.DetectLatest(ctx, selfupdate.ParseSlug(u.repository))
	if errors.Is(err, selfupdate.ErrValidationAssetNotFound) {
		return microerror.Mask(fmt.Errorf("the latest release of %s has no signature bundle for this platform's binary, so it cannot be verified; refusing to install it: %w", u.repository, err))
	} else if err != nil {
		return microerror.Mask(err)
	}

	if !found {
		return microerror.Maskf(versionNotFoundError, "couldn't find the latest version and/or release assets on GitHub, probably due to token without access to the repository %s.", u.repository)
	}

	latestVersion, err := semver.Parse(latest.Version())
	if err != nil {
		return microerror.Mask(err)
	}

	if latestVersion.LTE(u.currentVersion) {
		// Nothing newer to install.
		return nil
	}

	exe, err := executablePath()
	if err != nil {
		return microerror.Mask(err)
	}

	err = u.installer.UpdateTo(ctx, latest, exe)
	if err != nil {
		return microerror.Mask(fmt.Errorf("update failed, %s is unchanged: %w", exe, err))
	}

	return nil
}

// GetLatest returns the latest version available in the
// source repository, and if we can upgrade to that version
// or not (it can be equal to the current version).
func (u *Updater) GetLatest() (version string, err error) {
	latestVersion, err := u.getLatestVersion()
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

func (u *Updater) getLatestVersion() (semver.Version, error) {
	allowCache := len(u.cacheDir) > 0

	if allowCache && !u.cache.IsExpired() {
		version, err := semver.Parse(u.cache.LatestVersion)
		// If this not a valid semver version, then it means
		// that the someone fiddled with the cache file. We'll
		// just override it.
		if err == nil {
			return version, nil
		}
	}

	latest, found, err := u.lookup.DetectLatest(context.Background(), selfupdate.ParseSlug(u.repository))
	if err != nil {
		return semver.Version{}, microerror.Mask(err)
	}

	if !found {
		return semver.Version{}, microerror.Maskf(versionNotFoundError, "couldn't find the latest version and/or release assets on GitHub, probably due to token without access to the repository %s.", u.repository)
	}

	latestVersion, err := semver.Parse(latest.Version())
	if err != nil {
		return semver.Version{}, microerror.Mask(err)
	}

	if allowCache {
		u.cache.LatestVersion = latestVersion.String()

		err = u.cache.Persist(u.cacheDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to persist the cache with error: %s", err)
		}
	}

	return latestVersion, nil
}

func (u *Updater) parseRepoFromURL(sourceURL string) (string, error) {
	repo, err := url.Parse(sourceURL)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return strings.TrimPrefix(repo.Path, "/"), nil
}
