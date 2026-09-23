// Package versiongate is the check that precedes every devctl command but
// the version commands: an outdated devctl refuses to run until it is
// updated, or until DEVCTL_UNSAFE_FORCE_VERSION names the running version.
package versiongate

import (
	"fmt"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/updater"
)

// Check returns nil when this build is the latest release or
// DEVCTL_UNSAFE_FORCE_VERSION names it, and otherwise the error naming the
// release and the two ways on. The latest release is read from the version
// cache unless noCache, and from GitHub when the cache is older than an hour.
func Check(noCache bool) error {
	if project.Version() == env.DevctlUnsafeForceVersion.Val() {
		return nil
	}

	var cacheDir string
	if !noCache {
		cacheDir = env.ConfigDir.Val()
	}
	u, err := updater.New(updater.Config{
		GithubToken:    env.GitHubToken.Val(),
		CurrentVersion: project.Version(),
		RepositoryURL:  project.Source(),
		CacheDir:       cacheDir,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	latest, err := u.GetLatest()
	if updater.IsHasNewVersion(err) {
		return fmt.Errorf("version %[2]s of %[1]s is released; this is %[3]s: update with `%[1]s version update`, or run this version anyway with %[4]s=%[3]s",
			project.Name(), latest, project.Version(), env.DevctlUnsafeForceVersion.Key())
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
