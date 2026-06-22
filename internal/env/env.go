package env

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/project"
)

var (
	ConfigDir                = configDir{}
	DevctlUnsafeForceVersion = devctlUnsafeForceVersion{}
	FlatcarChannel           = flatcarChannel{}
	FlatcarReleasesURL       = flatcarReleasesURL{}
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

type flatcarChannel struct{}

func (flatcarChannel) Key() string { return "FLATCAR_CHANNEL" }

// Val returns the Flatcar release channel to fetch versions from. Defaults to "stable".
func (flatcarChannel) Val() string {
	if c := os.Getenv(flatcarChannel{}.Key()); c != "" {
		return c
	}

	return "stable"
}

type flatcarReleasesURL struct{}

func (flatcarReleasesURL) Key() string { return "FLATCAR_RELEASES_URL" }

// Val returns the URL of the Flatcar releases JSON manifest. If FLATCAR_RELEASES_URL
// is not set, it is derived from the configured channel.
func (flatcarReleasesURL) Val() string {
	if u := os.Getenv(flatcarReleasesURL{}.Key()); u != "" {
		return u
	}

	return fmt.Sprintf("https://www.flatcar.org/releases-json/releases-%s.json", FlatcarChannel.Val())
}

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
