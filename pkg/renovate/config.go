package renovate

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
)

// ConfigPaths are the configuration files Renovate reads in a repository,
// relative to its root, the generated renovate.json5 first.
var ConfigPaths = []string{"renovate.json5", "renovate.json", ".github/renovate.json5", ".github/renovate.json", ".renovaterc", ".renovaterc.json"}

// HasConfig says whether the repository checked out at dir carries a
// Renovate configuration at any of [ConfigPaths].
func HasConfig(dir string) (bool, error) {
	for _, p := range ConfigPaths {
		info, err := os.Stat(filepath.Join(dir, p))
		if err == nil && !info.IsDir() {
			return true, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return false, microerror.Mask(err)
		}
	}
	return false, nil
}
