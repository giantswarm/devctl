package rolloutwait

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

// bare is a chart version without a leading v and without build metadata:
// Flux reports 0.48.1+6a72bd1e624f for the OCI artifact of 0.48.1.
func bare(version string) string {
	version = strings.TrimPrefix(version, "v")
	version, _, _ = strings.Cut(version, "+")
	return version
}

// atLeast reports whether running is target or newer; a running version
// that is not semver never is.
func atLeast(running, target string) bool {
	r, err := semver.StrictNewVersion(bare(running))
	if err != nil {
		return false
	}
	t, err := semver.StrictNewVersion(bare(target))
	if err != nil {
		return false
	}
	return !r.LessThan(t)
}

// allows reports whether a semver range admits version; an empty range or *
// admits every release.
func allows(constraint, version string) (bool, error) {
	if constraint == "" || constraint == "*" {
		return true, nil
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, err
	}
	v, err := semver.StrictNewVersion(bare(version))
	if err != nil {
		return false, err
	}
	return c.Check(v), nil
}
