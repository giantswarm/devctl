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

func TestCommandDetectsActionManifests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
	}{
		{name: "root action.yml", files: []string{"action.yml"}},
		{name: "root action.yaml", files: []string{"action.yaml"}},
		{name: "workflows", files: []string{".github/workflows/fixture.yml"}},
		{name: "all action locations", files: []string{"action.yml", "action.yaml", ".github/workflows/fixture.yml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.WriteFile("Dockerfile", []byte("FROM scratch\n"), 0o600))
			for _, path := range tc.files {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
				content := "name: Fixture\ndescription: Synthetic composite action\nruns:\n  using: composite\n  steps:\n    - uses: fixture/action@v1\n"
				if filepath.Dir(path) != "." {
					content = "name: Fixture\non: push\njobs:\n  fixture:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: fixture/action@v1\n"
				}
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SetArgs(nil)
			require.NoError(t, cmd.Execute())

			content, err := os.ReadFile(".github/dependabot.yml")
			require.NoError(t, err)
			var config struct {
				Updates []struct {
					Ecosystem string `yaml:"package-ecosystem"`
				} `yaml:"updates"`
			}
			require.NoError(t, yaml.Unmarshal(content, &config))
			var ecosystems []string
			for _, update := range config.Updates {
				ecosystems = append(ecosystems, update.Ecosystem)
			}
			require.ElementsMatch(t, []string{"docker", "github-actions"}, ecosystems)
		})
	}
}
