package project

import (
	"runtime/debug"
	"strings"
)

// dev is the default for unset build identifiers. Local `go build`
// invocations without ldflags keep this so `devctl version` stays printable.
const dev = "dev"

// devel is the module version runtime/debug reports for a build that carries
// no resolvable VCS tag (no .git, or built outside a module checkout).
const devel = "(devel)"

var (
	description = "Command line tool for the development daily business."
	// gitSHA is injected at link time via `-X` ldflags by architect-orb's
	// `go-build` job (from `CIRCLE_SHA1`). architect does NOT inject `version`;
	// the version is instead derived at runtime from the Go build info (see
	// Version), which the toolchain stamps from the VCS tag — a clean semver
	// when the build sits exactly on a tag (as release builds do, since they
	// run on the tagged commit). `version` is left as an escape hatch for an
	// explicit `-X` override (the Makefile sets it from `gitsemver get`) but is
	// normally unset for architect-built release binaries.
	gitSHA  = "n/a"
	name    = "devctl"
	source  = "https://github.com/giantswarm/devctl"
	version = dev
)

func Description() string {
	return description
}

func GitSHA() string {
	return gitSHA
}

func Name() string {
	return name
}

func Source() string {
	return source
}

// Version returns the best human-readable build identifier available, in
// order: an explicitly injected `version` ldflag, the VCS version stamped into
// the Go build info, the injected commit SHA, and finally the placeholder
// "dev". The leading "v" is trimmed so the result is a bare semver that
// `pkg/updater` (blang/semver.Parse) and the self-update version comparison can
// consume.
func Version() string {
	if version != dev && version != "" {
		return version
	}
	if v := buildInfoVersion(); v != "" {
		return v
	}
	if gitSHA != "n/a" {
		return gitSHA
	}
	return dev
}

// buildInfoVersion reads the main module version the Go toolchain embedded from
// version control, trimming the leading "v" so it parses as a bare semver. It
// returns "" when no usable version is present — either no build info, or the
// "(devel)" placeholder a tag-less build produces — so Version can fall through
// to the next source.
var buildInfoVersion = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := info.Main.Version
	if v == "" || v == devel {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}
