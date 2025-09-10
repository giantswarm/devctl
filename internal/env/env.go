package env

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/project"
)

var (
	ConfigDir                = configDir{}
	DevctlUnsafeForceVersion = devctlUnsafeForceVersion{}
	GitHubToken              = gitHubToken{}
)

type configDir struct{}

func (configDir) Val() string {
	s := os.Getenv("XDG_CONFIG_HOME")
	if len(s) == 0 {
		var err error
		s, err = os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		s = filepath.Join(s, ".config")
	}

	return filepath.Join(s, project.Name())
}

type devctlUnsafeForceVersion struct{}

func (devctlUnsafeForceVersion) Key() string { return "DEVCTL_UNSAFE_FORCE_VERSION" } // nolint:gosec
func (devctlUnsafeForceVersion) Val() string { return os.Getenv(devctlUnsafeForceVersion{}.Key()) }

type gitHubToken struct{}

// Tries to get the GitHub token from environment variables.
func (gitHubToken) Val() string {
	// Try DEVCTL_GITHUB_TOKEN first.
	if token := os.Getenv("DEVCTL_GITHUB_TOKEN"); token != "" {
		return token
	}

	// Try GITHUB_TOKEN.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}

	// Fallback to OPSCTL_GITHUB_TOKEN.
	if token := os.Getenv("OPSCTL_GITHUB_TOKEN"); token != "" {
		return token
	}

	return ""
}
