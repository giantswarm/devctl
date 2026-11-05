package precommit

import (
	"io"
	"os"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func Test_Run_GoImportsPrefix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		repo    string
		module  string
		want    string
		wantErr string
	}{
		{name: "documented bare repository name", repo: "fixture", want: "github.com/giantswarm/fixture"},
		{name: "explicit full module path", repo: "example.invalid/team/fixture/v2", want: "example.invalid/team/fixture/v2"},
		{name: "detect full module path", module: "module example.invalid/team/fixture/v2\n\ngo 1.26.0\n", want: "example.invalid/team/fixture/v2"},
		{name: "detect local module name", module: "module fixture\n\ngo 1.26.0\n", want: "fixture"},
		{name: "bare override bypasses broken go.mod", repo: "fixture", module: "module\n", want: "github.com/giantswarm/fixture"},
		{name: "full override bypasses broken go.mod", repo: "example.invalid/fixture", module: "module\n", want: "example.invalid/fixture"},
		{name: "no go.mod", wantErr: "failed to read go.mod"},
		{name: "broken go.mod", module: "module\n", wantErr: "failed to parse go.mod"},
		{name: "missing module directive", module: "go 1.26.0\n", wantErr: "does not declare a module path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.module != "" {
				require.NoError(t, os.WriteFile("go.mod", []byte(tc.module), 0o600))
			}
			const original = "# original config\n"
			require.NoError(t, os.WriteFile(".pre-commit-config.yaml", []byte(original), 0o600))
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			command.SilenceErrors = true
			command.SilenceUsage = true
			args := []string{"--language=go"}
			if tc.repo != "" {
				args = append(args, "--repo-name="+tc.repo)
			}
			command.SetArgs(args)
			err = command.Execute()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				got, err := os.ReadFile(".pre-commit-config.yaml")
				require.NoError(t, err)
				require.Equal(t, original, string(got))
				require.NoFileExists(t, ".github/workflows/zz_generated.pre-commit.yaml")
				return
			}
			require.NoError(t, err)
			data, err := os.ReadFile(".pre-commit-config.yaml")
			require.NoError(t, err)
			var config struct {
				Repos []struct {
					Hooks []struct {
						ID   string   `json:"id"`
						Args []string `json:"args"`
					} `json:"hooks"`
				} `json:"repos"`
			}
			require.NoError(t, yaml.Unmarshal(data, &config))
			var prefixes [][]string
			for _, repo := range config.Repos {
				for _, hook := range repo.Hooks {
					if hook.ID == "go-imports" {
						prefixes = append(prefixes, hook.Args)
					}
				}
			}
			require.Equal(t, [][]string{{"-local", tc.want}}, prefixes)
		})
	}
}
