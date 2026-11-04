package renovate

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// The scaffold and align-files must recognize the newly supported config
// formats as existing configuration rather than generate a competing file.
func TestHasConfigAdditionalFormats(t *testing.T) {
	paths := []string{"renovate.jsonc", ".github/renovate.jsonc", ".renovaterc.jsonc", ".renovaterc.json5"}
	for i, path := range paths {
		t.Run("case "+strconv.Itoa(i)+": "+path, func(t *testing.T) {
			dir := t.TempDir()
			found, err := HasConfig(dir)
			require.NoError(t, err)
			require.False(t, found)
			config := filepath.Join(dir, filepath.FromSlash(path))
			require.NoError(t, os.MkdirAll(filepath.Dir(config), 0750))
			require.NoError(t, os.WriteFile(config, []byte("{}"), 0600))
			found, err = HasConfig(dir)
			require.NoError(t, err)
			require.True(t, found)
		})
	}
}
