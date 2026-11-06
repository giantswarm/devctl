package dependabot

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCommandDetectsGoManifests(t *testing.T) {
	for _, tc := range []struct {
		name      string
		files     map[string]string
		args      []string
		wantGomod bool
	}{
		{
			name:      "module without a checksum file",
			files:     map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.26.0\n"},
			wantGomod: true,
		},
		{
			name:      "module with a checksum file",
			files:     map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.26.0\n", "go.sum": ""},
			wantGomod: true,
		},
		{
			name: "workspace with a nested module",
			files: map[string]string{
				"go.work":        "go 1.26.0\n\nuse ./service\n",
				"service/go.mod": "module example.test/service\n\ngo 1.26.0\n",
			},
			wantGomod: true,
		},
		{
			name: "module and workspace produce one update entry",
			files: map[string]string{
				"go.mod":  "module example.test/fixture\n\ngo 1.26.0\n",
				"go.work": "go 1.26.0\n\nuse .\n",
			},
			wantGomod: true,
		},
		{
			name:  "a checksum file alone is not a manifest",
			files: map[string]string{"go.sum": ""},
		},
		{
			name:  "explicit ecosystems override module detection",
			files: map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.26.0\n"},
			args:  []string{"--ecosystems=docker"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			// Keep a second ecosystem present so missing Go detection cannot
			// be confused with the behavior of an empty ecosystem selection.
			require.NoError(t, os.WriteFile("Dockerfile", []byte("FROM scratch\n"), 0o600))
			for name, content := range tc.files {
				require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o750))
				require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.Execute())

			content, err := os.ReadFile(".github/dependabot.yml")
			require.NoError(t, err)
			var config struct {
				Updates []struct {
					Ecosystem string `yaml:"package-ecosystem"`
					Directory string `yaml:"directory"`
				} `yaml:"updates"`
			}
			require.NoError(t, yaml.Unmarshal(content, &config))
			var ecosystems []string
			for _, update := range config.Updates {
				require.Equal(t, "/", update.Directory)
				ecosystems = append(ecosystems, update.Ecosystem)
			}
			want := []string{"docker"}
			if tc.wantGomod {
				want = append(want, "gomod")
			}
			require.ElementsMatch(t, want, ecosystems)
		})
	}
}
