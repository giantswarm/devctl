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

func TestCommandDetectsPythonManifests(t *testing.T) {
	fixtures := map[string]string{
		"requirements.txt": "fixture-package==1.0.0\n",
		"requirements.in":  "fixture-package==1.0.0\n",
		"Pipfile":          "[packages]\nfixture-package = \"==1.0.0\"\n",
		"setup.cfg":        "[metadata]\nname = fixture\n[options]\ninstall_requires =\n    fixture-package==1.0.0\n",
		"pyproject.toml":   "[project]\nname = \"fixture\"\nversion = \"1.0.0\"\ndependencies = [\"fixture-package==1.0.0\"]\n",
		"setup.py":         "from setuptools import setup\nsetup(name='fixture', install_requires=['fixture-package==1.0.0'])\n",
		".abs/main.yaml":   "version: 1\n",
	}
	for _, name := range []string{"requirements.txt", "requirements.in", "Pipfile", "setup.cfg", "pyproject.toml", "setup.py", ".abs/main.yaml", "all manifests", "explicit override"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.WriteFile("Dockerfile", []byte("FROM scratch\n"), 0o600))
			for path, content := range fixtures {
				if name != "all manifests" && name != "explicit override" && path != name {
					continue
				}
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SetArgs(nil)
			if name == "explicit override" {
				cmd.SetArgs([]string{"--ecosystems=docker"})
			}
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
			want := []string{"docker", "pip"}
			if name == "explicit override" {
				want = []string{"docker"}
			}
			require.ElementsMatch(t, want, ecosystems)
		})
	}
}
