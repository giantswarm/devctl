package reposetup

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A fork line carries its upstream's files plus the carried patches: no
// generator runs for it and generated CI has no job for it, whatever its
// language.
func TestGenCommandsNothingForAForkLine(t *testing.T) {
	fork := Fields{Name: "upstream-fork", Gen: &GenFields{Flavours: []string{"fork"}, Language: "go"}}
	require.Empty(t, genCommands(fork, genContext{}))
	require.False(t, hasCIJob(fork))

	service := Fields{Name: "service", Gen: &GenFields{Flavours: []string{"app"}, Language: "go"}}
	require.NotEmpty(t, genCommands(service, genContext{}))
	require.True(t, hasCIJob(service))
}

// Every declaration that generates at all gets the Renovate line, last;
// --circleci-generated follows gen.ci.generate. A fork line and an entry
// without gen get none.
func TestGenCommandsRenovate(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name   string
		fields Fields
		want   []string // nil: no Renovate line
	}{
		{
			name:   "generated CI",
			fields: Fields{Name: "service", Gen: &GenFields{Flavours: []string{"app"}, Language: "go", CI: &CIFields{Generate: &on}}},
			want:   []string{"devctl", "gen", "renovate", "--language", "go", "--circleci-generated", "--repo-name", "service"},
		},
		{
			name:   "no generated CI",
			fields: Fields{Name: "configs", Gen: &GenFields{Flavours: []string{"generic"}, Language: "generic", CI: &CIFields{Generate: &off}}},
			want:   []string{"devctl", "gen", "renovate", "--language", "generic", "--repo-name", "configs"},
		},
		{
			name:   "no gen.ci",
			fields: Fields{Name: "tool", Gen: &GenFields{Flavours: []string{"generic"}, Language: "python"}},
			want:   []string{"devctl", "gen", "renovate", "--language", "python", "--repo-name", "tool"},
		},
		{
			name: "no generated CI, deprecated, with chore reviewers",
			fields: Fields{Name: "configs", Lifecycle: lifecycleDeprecated, ChoreReviewers: []string{"team:team-honeybadger"},
				Gen: &GenFields{Flavours: []string{"customer"}, Language: "generic", CI: &CIFields{Generate: &off}}},
			want: []string{"devctl", "gen", "renovate", "--language", "generic", "--repo-name", "configs", "--deprecated", "-r", "team:team-honeybadger"},
		},
		{
			name:   "fork line",
			fields: Fields{Name: "upstream-fork", Gen: &GenFields{Flavours: []string{"fork"}, Language: "go"}},
		},
		{
			name:   "no gen",
			fields: Fields{Name: "plain"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commands := genCommands(tc.fields, genContext{})
			var got []string
			for _, c := range commands {
				if c[2] == genRenovate {
					got = c
				}
			}
			require.Equal(t, tc.want, got)
			if tc.want != nil {
				require.Equal(t, tc.want, commands[len(commands)-1], "Renovate is the last line, as align-files runs it")
			}
		})
	}
}

// Every gen.ci key of the embedded schema reaches the `devctl gen circleci`
// line the scaffold runs as the flag its description names, as align-files
// passes it: a key the schema admits and the scaffold drops leaves a created
// repository's first CircleCI configuration short of what the entry
// declares.
func TestGenCommandsPassEveryGenCIKey(t *testing.T) {
	var schema struct {
		Items struct {
			Properties struct {
				Gen struct {
					Properties struct {
						CI schemaNode `json:"ci"`
					} `json:"properties"`
				} `json:"gen"`
			} `json:"properties"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(embeddedSchema, &schema))

	// The keys that are no flag of the CircleCI generator.
	notCircleCIFlags := map[string]bool{
		"generate":              true, // turns the line on
		"releaseWorkflow":       true, // a flag of gen workflows
		"requireCircleCIChecks": true, // the protection step's
		"atsBranchOnly":         true, // deprecated, the generator ignores it
		"templateContent":       true, // the reconciler's: no pipeline to generate
	}
	flagName := regexp.MustCompile(`(?:^|[^a-z0-9-])(--[a-z0-9]+(?:-[a-z0-9]+)*)`)

	var keys int
	var walk func(path []string, n schemaNode)
	walk = func(path []string, n schemaNode) {
		for key, p := range n.Properties {
			at := append(append([]string{}, path...), key)
			if p.Type == "object" && len(p.Properties) > 0 {
				walk(at, p)
				continue
			}
			if len(at) == 1 && notCircleCIFlags[key] {
				continue
			}
			keys++
			m := flagName.FindStringSubmatch(p.Description)
			require.NotNil(t, m, "gen.ci.%s names no flag in its description", strings.Join(at, "."))

			ci := map[string]any{"generate": true}
			leaf := ci
			for _, k := range at[:len(at)-1] {
				next := map[string]any{}
				leaf[k] = next
				leaf = next
			}
			leaf[key] = sampleValue(p)
			doc, err := yaml.Marshal(map[string]any{"name": "sample", "gen": map[string]any{"flavours": []string{"app"}, "language": "go", "ci": ci}})
			require.NoError(t, err)
			var f Fields
			require.NoError(t, yaml.Unmarshal(doc, &f))

			line := circleCILine(genCommands(f, genContext{}))
			require.NotNil(t, line, "no gen circleci line for gen.ci.%s", strings.Join(at, "."))
			found := false
			for _, arg := range line {
				if arg == m[1] || strings.HasPrefix(arg, m[1]+"=") {
					found = true
				}
			}
			require.True(t, found, "gen.ci.%s does not reach the gen circleci line as %s: %v", strings.Join(at, "."), m[1], line)
		}
	}
	walk(nil, schema.Items.Properties.Gen.Properties.CI)
	require.NotZero(t, keys)
}

// schemaNode is the part of a JSON schema node the key walk reads.
type schemaNode struct {
	Type        string                `json:"type"`
	Description string                `json:"description"`
	Properties  map[string]schemaNode `json:"properties"`
}

// sampleValue is a value of the schema type that turns the key on.
func sampleValue(n schemaNode) any {
	switch n.Type {
	case "boolean":
		return true
	case "object":
		return map[string]string{"linux/arm64": "arm.large"}
	default:
		return "sample"
	}
}

func circleCILine(commands [][]string) []string {
	for _, c := range commands {
		if len(c) > 2 && c[2] == genCircleCI {
			return c
		}
	}
	return nil
}
