package renovate

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
)

// ConfigPaths are the configuration files Renovate reads in a repository,
// relative to its root, in Renovate's precedence order for GitHub.
var ConfigPaths = []string{
	"renovate.json", "renovate.jsonc", "renovate.json5",
	".github/renovate.json", ".github/renovate.jsonc", ".github/renovate.json5",
	".renovaterc", ".renovaterc.json", ".renovaterc.jsonc", ".renovaterc.json5",
}

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
