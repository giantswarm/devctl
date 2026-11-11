package dependabot

import (
	"io"
	"os"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCommandRejectsDuplicateEcosystems(t *testing.T) {
	for _, args := range [][]string{
		{"--ecosystems=docker,docker"},
		{"--ecosystems=docker", "--ecosystems=gomod,docker"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".github", 0o750))
			const original = "version: 2\nupdates:\n  - package-ecosystem: docker\n    directory: /\n    schedule:\n      interval: daily\n"
			require.NoError(t, os.WriteFile(".github/dependabot.yml", []byte(original), 0o600))
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(args)
			err = cmd.Execute()
			content, readErr := os.ReadFile(".github/dependabot.yml")
			require.NoError(t, readErr)
			require.Equal(t, original, string(content), "invalid selections must preserve the working configuration")
			require.True(t, IsInvalidFlag(err), "expected invalid flag error, got %v", err)
			require.ErrorContains(t, err, "docker")
		})
	}
}

func TestCommandAcceptsDistinctEcosystems(t *testing.T) {
	t.Chdir(t.TempDir())
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	require.NoError(t, err)
	// Explicit selection works without local manifests. Flag repetition
	// appends distinct ecosystems and must remain supported.
	cmd.SetArgs([]string{
		"--ecosystems=docker,gomod",
		"--ecosystems=github-actions,npm,pip",
		"--interval=monthly",
		"--reviewers=fixture-org/reviewers,fixture-user",
	})
	require.NoError(t, cmd.Execute())
	content, err := os.ReadFile(".github/dependabot.yml")
	require.NoError(t, err)
	var config struct {
		Version int `yaml:"version"`
		Updates []struct {
			Ecosystem string   `yaml:"package-ecosystem"`
			Directory string   `yaml:"directory"`
			Reviewers []string `yaml:"reviewers"`
			Schedule  struct {
				Interval string `yaml:"interval"`
			} `yaml:"schedule"`
		} `yaml:"updates"`
	}
	require.NoError(t, yaml.Unmarshal(content, &config))
	require.Equal(t, 2, config.Version)
	var ecosystems []string
	for _, update := range config.Updates {
		require.Equal(t, "/", update.Directory)
		require.Equal(t, "monthly", update.Schedule.Interval)
		require.ElementsMatch(t, []string{"fixture-org/reviewers", "fixture-user"}, update.Reviewers)
		ecosystems = append(ecosystems, update.Ecosystem)
	}
	require.ElementsMatch(t, []string{"docker", "gomod", "github-actions", "npm", "pip"}, ecosystems)
}
