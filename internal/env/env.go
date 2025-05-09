package env

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/project"
)

var (
	ConfigDir                = configDir{}
	DevctlUnsafeForceVersion = devctlUnsafeForceVersion{}
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
