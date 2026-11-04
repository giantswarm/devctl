package precommit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/cmd/gen/precommit"
)

// Follow the command printed by a failing schema hook, as in mcp-prometheus.
// The fixture has no remote schema references and runs no installed hooks/tools.
func TestSchemaRegenerationAdvice(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "case 0: Go module autodetection",
			args: []string{"--language=go", "--flavors=helmchart"},
		},
		{
			name: "case 1: all generation options",
			args: []string{"--language=go", "--repo-name=github.com/example/override/v2", "--flavors=md,bash,helmchart", "--k8s-schema-version=v1.29.0", "--go-generate"},
		},
		{
			name: "case 2: generic chart repository",
			args: []string{"--language=generic", "--repo-name=mcp-prometheus", "--flavors=helmchart"},
		},
		{
			name: "case 3: omitted language",
			args: []string{"--flavors=helmchart"},
		},
		{
			name: "case 4: literal shell characters in an override",
			args: []string{"--language=go", "--repo-name=example's $(printf injected) `printf expanded`", "--flavors=helmchart"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll("helm/mcp-prometheus", 0o750))
			const module = "github.com/giantswarm/mcp-prometheus/v2"
			require.NoError(t, os.WriteFile("go.mod", []byte("module "+module+"\n"), 0o600))
			require.NoError(t, os.WriteFile("helm/mcp-prometheus/Chart.yaml", []byte("name: mcp-prometheus\n"), 0o600))
			const valuesPath = "helm/mcp-prometheus/values.yaml"
			const schemaPath = "helm/mcp-prometheus/values.schema.json"
			require.NoError(t, os.WriteFile(valuesPath, []byte("replicaCount: 1\n"), 0o600))
			require.NoError(t, executeRegeneration(t, tc.args))
			config := readRegenerationFile(t, ".pre-commit-config.yaml")
			workflow := readRegenerationFile(t, ".github/workflows/zz_generated.pre-commit.yaml")
			originalSchema := readRegenerationFile(t, schemaPath)

			var parsed struct {
				Repos []struct {
					Hooks []struct {
						ID   string   `json:"id"`
						Args []string `json:"args"`
					} `json:"hooks"`
				} `json:"repos"`
			}
			require.NoError(t, yaml.Unmarshal(config, &parsed))
			var script string
			for _, repo := range parsed.Repos {
				for _, hook := range repo.Hooks {
					if hook.ID == "helm-schema-mcp-prometheus" {
						require.Len(t, hook.Args, 1)
						script = hook.Args[0]
					}
				}
			}
			// Execute the hook's real diff/failure branch against an empty local
			// scratch file. The generation/normalization steps need no stubs.
			failure := strings.LastIndex(script, "if ! diff ")
			require.GreaterOrEqual(t, failure, 0, "schema hook must remain present")
			require.NoError(t, os.WriteFile("scratch.json", nil, 0o600))
			cmd := exec.Command(shell, "-c", "tmp=scratch.json\n"+script[failure:]) // #nosec G204 -- generated hook fragment in a temporary offline fixture
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			var exit *exec.ExitError
			require.True(t, errors.As(cmd.Run(), &exit), "a stale schema must fail its hook")
			require.Equal(t, 1, exit.ExitCode())
			require.Contains(t, stderr.String(), "repository root")
			require.Equal(t, originalSchema, readRegenerationFile(t, schemaPath), "the hook must remain read-only")

			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			advice := lines[len(lines)-1]
			require.True(t, strings.HasPrefix(advice, "devctl gen precommit "), "missing complete recovery command: %s", advice)
			// Let the shell interpret the printed command, then hand its exact
			// arguments to the public Cobra command without building a second CLI.
			cmd = exec.Command(shell, "-c", "set -- "+advice+`; printf '%s\000' "$@"`) // #nosec G204 -- generated advice from fixed test options, never user input
			words, err := cmd.Output()
			require.NoError(t, err)
			args := strings.Split(strings.TrimSuffix(string(words), "\x00"), "\x00")
			require.GreaterOrEqual(t, len(args), 3)
			require.Equal(t, []string{"devctl", "gen", "precommit"}, args[:3])
			for _, arg := range tc.args {
				require.Contains(t, args[3:], arg, "every supplied option must survive both shells")
			}
			if tc.name == testCases[0].name {
				require.Contains(t, args, "--repo-name="+module)
			}

			require.NoError(t, os.WriteFile(valuesPath, []byte("replicaCount: 1\nnewSetting: fixture\n"), 0o600))
			require.NoError(t, executeRegeneration(t, args[3:]))
			require.Equal(t, config, readRegenerationFile(t, ".pre-commit-config.yaml"))
			require.Equal(t, workflow, readRegenerationFile(t, ".github/workflows/zz_generated.pre-commit.yaml"))
			schema := readRegenerationFile(t, schemaPath)
			require.NotEqual(t, originalSchema, schema)
			var generated struct {
				Properties map[string]struct {
					Type json.RawMessage `json:"type"`
				} `json:"properties"`
			}
			require.NoError(t, json.Unmarshal(schema, &generated))
			require.Equal(t, json.RawMessage(`"string"`), generated.Properties["newSetting"].Type)
			require.Equal(t, json.RawMessage(`"integer"`), generated.Properties["replicaCount"].Type)
		})
	}
}

func executeRegeneration(t *testing.T, args []string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	command, err := precommit.New(precommit.Config{Logger: logger, Stderr: io.Discard, Stdout: io.Discard})
	require.NoError(t, err)
	command.SetArgs(args)
	return command.ExecuteContext(t.Context())
}

func readRegenerationFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path) // #nosec G304 -- fixed paths in a temporary offline fixture
	require.NoError(t, err)
	return content
}
