// Package scenario is the format of e2e/scenarios/<slug>/: scenario.yaml
// (the command line, the environment, the mocks' response sequences) and
// expected.json (the exit code and the JSON document, "*" for a value the
// scenario does not pin). e2e/README.md documents it for the people who add
// scenarios; this package is where the harness reads it.
package scenario

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

// File names of a scenario directory.
const (
	ScenarioFile = "scenario.yaml"
	ExpectedFile = "expected.json"
)

// DefaultTimeout bounds one scenario's run of the binary.
const DefaultTimeout = 60 * time.Second

// Scenario is scenario.yaml.
type Scenario struct {
	// Description says what the scenario proves, one line.
	Description string `yaml:"description"`
	// Args is devctl's command line without the binary.
	Args []string `yaml:"args"`
	// Env is added to the environment the harness builds; a variable named
	// here replaces the harness's value.
	Env map[string]string `yaml:"env"`
	// Timeout for the run; DefaultTimeout when left out.
	Timeout time.Duration `yaml:"timeout"`
	// Keyring is written as JSON to the file DEVCTL_KEYRING_FILE names; left
	// out, the file does not exist.
	Keyring any `yaml:"keyring"`
	// GitHub, CircleCI, Registry and PrivateRegistry script the mocks.
	GitHub          Mock         `yaml:"github"`
	CircleCI        Mock         `yaml:"circleci"`
	Registry        RegistryMock `yaml:"registry"`
	PrivateRegistry RegistryMock `yaml:"privateRegistry"`
}

// Mock is one mock's script.
type Mock struct {
	Routes sequence.Routes `yaml:"routes"`
}

// RegistryMock is a registry's script and state.
type RegistryMock struct {
	Mock `yaml:",inline"`
	// StaleLogin makes the registry refuse every request that carries
	// credentials with 401; anonymous requests are served by the routes.
	StaleLogin bool `yaml:"staleLogin"`
}

// Expected is expected.json.
type Expected struct {
	// ExitCode the binary must exit with.
	ExitCode int `json:"exitCode"`
	// JSON is the document stdout must carry, "*" standing for any value;
	// left out, stdout is not compared.
	JSON json.RawMessage `json:"json"`
}

// ComparesJSON says whether stdout is compared.
func (e Expected) ComparesJSON() bool {
	return len(e.JSON) > 0 && !bytes.Equal(bytes.TrimSpace(e.JSON), []byte("null"))
}

// Load reads a scenario directory. Unknown fields in either file are errors,
// so a misspelt key fails the scenario instead of being ignored.
func Load(dir string) (*Scenario, *Expected, error) {
	scenario := &Scenario{Timeout: DefaultTimeout}
	if err := decodeYAML(filepath.Join(dir, ScenarioFile), scenario); err != nil {
		return nil, nil, err
	}
	if len(scenario.Args) == 0 {
		return nil, nil, fmt.Errorf("%s: args must name the command", filepath.Join(dir, ScenarioFile))
	}
	expected := &Expected{}
	if err := decodeJSON(filepath.Join(dir, ExpectedFile), expected); err != nil {
		return nil, nil, err
	}
	return scenario, expected, nil
}

// Discover lists the directories under root that hold a scenario.yaml,
// sorted by name.
func Discover(root string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "*", ScenarioFile))
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(matches))
	for _, match := range matches {
		dirs = append(dirs, filepath.Dir(match))
	}
	sort.Strings(dirs)
	return dirs, nil
}

// Wildcard is the expected value that matches anything.
const Wildcard = "*"

// Match compares an actual JSON document with the expected one, both as
// decoded by encoding/json: objects need the same keys with matching values,
// arrays the same length with matching elements, scalars equality, and the
// Wildcard matches any value. The error names the first difference by path.
func Match(expected, actual any) error {
	return match("$", expected, actual)
}

func match(path string, expected, actual any) error {
	switch want := expected.(type) {
	case string:
		if want == Wildcard {
			return nil
		}
		got, ok := actual.(string)
		if !ok || got != want {
			return difference(path, want, actual)
		}
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return difference(path, want, actual)
		}
		for key := range got {
			if _, ok := want[key]; !ok {
				return fmt.Errorf("at %s.%s: key not expected, value %s", path, key, render(got[key]))
			}
		}
		for _, key := range sortedKeys(want) {
			value, ok := got[key]
			if !ok {
				return fmt.Errorf("at %s.%s: key missing, want %s", path, key, render(want[key]))
			}
			if err := match(path+"."+key, want[key], value); err != nil {
				return err
			}
		}
	case []any:
		got, ok := actual.([]any)
		if !ok {
			return difference(path, want, actual)
		}
		if len(got) != len(want) {
			return fmt.Errorf("at %s: want %d elements, got %d", path, len(want), len(got))
		}
		for i := range want {
			if err := match(fmt.Sprintf("%s[%d]", path, i), want[i], got[i]); err != nil {
				return err
			}
		}
	default:
		if !bytes.Equal(canonical(want), canonical(actual)) {
			return difference(path, want, actual)
		}
	}
	return nil
}

func difference(path string, want, got any) error {
	return fmt.Errorf("at %s: want %s, got %s", path, render(want), render(got))
}

func render(value any) string {
	return string(canonical(value))
}

func canonical(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return []byte(fmt.Sprintf("%v", value))
	}
	return data
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func decodeYAML(path string, into any) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func decodeJSON(path string, into any) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
