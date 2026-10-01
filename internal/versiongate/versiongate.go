// Package versiongate is the check that precedes every devctl command but
// the version commands: an outdated devctl refuses to run until it is
// updated, or until DEVCTL_UNSAFE_FORCE_VERSION names the running version.
package versiongate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/updater"
)

// Check returns nil when this build is the latest release or
// DEVCTL_UNSAFE_FORCE_VERSION names it, and otherwise the error naming the
// release and the two ways on. The latest release is read from the version
// cache unless noCache, and from GitHub when the cache is older than an hour.
func Check(noCache bool) error {
	return check(project.Version(), noCache, os.Stderr)
}

func check(version string, noCache bool, warnings io.Writer) error {
	if version == env.DevctlUnsafeForceVersion.Val() {
		return nil
	}

	u, err := newUpdater(version, noCache, warnings)
	if err != nil {
		return microerror.Mask(err)
	}

	latest, err := u.GetLatest()
	if updater.IsHasNewVersion(err) {
		return fmt.Errorf("version %[2]s of %[1]s is released; this is %[3]s: update with `%[1]s version update`, or run this version anyway with %[4]s=%[3]s",
			project.Name(), latest, version, env.DevctlUnsafeForceVersion.Key())
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// NewUpdater is the updater of this devctl: its releases on GitHub, read with
// the token of the App login or the environment (anonymously without one,
// the keychain opened only when GitHub is asked), and the version cache in
// the config directory unless noCache. The token's override warning goes to
// warnings.
func NewUpdater(noCache bool, warnings io.Writer) (*updater.Updater, error) {
	return newUpdater(project.Version(), noCache, warnings)
}

func newUpdater(version string, noCache bool, warnings io.Writer) (*updater.Updater, error) {
	var cacheDir string
	if !noCache {
		cacheDir = env.ConfigDir.Val()
	}
	u, err := updater.New(updater.Config{
		GithubToken:    githubToken(warnings),
		GitHubAPIURL:   agentcli.EndpointsFromEnv().GitHubAPIURL,
		CurrentVersion: version,
		RepositoryURL:  project.Source(),
		CacheDir:       cacheDir,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return u, nil
}

// githubToken is the optional token of a version lookup, a read of public
// releases: [authstore.ResolveGitHub]'s token, its override warning written
// to warnings once, or "" to read anonymously when none is configured. Any
// other error of the resolver is returned.
func githubToken(warnings io.Writer) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		token, err := authstore.ResolveGitHub(ctx)
		if errors.Is(err, authstore.ErrAuthRequired) {
			return "", nil
		} else if err != nil {
			return "", err
		}
		token.WarnOnce(warnings)
		return token.Value, nil
	}
}
