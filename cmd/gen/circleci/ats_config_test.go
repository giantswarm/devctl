package circleci

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/ats"
)

func runATSConfigCommand(t *testing.T, args ...string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"--repo-name", "example", "--language", "generic"}, args...))
	return cmd.ExecuteContext(t.Context())
}

func Test_Run_ATSConfig(t *testing.T) {
	testCases := []struct {
		name    string
		args    []string
		legacy  bool
		release bool
	}{
		{name: "case 0: default ATS"},
		{name: "case 1: release tests", args: []string{"--ats-on-release"}, release: true},
		{name: "case 2: legacy ATS", args: []string{"--ats-version", "0.15.1"}, legacy: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			args := append([]string{"--flavour", "app"}, tc.args...)
			if err := runATSConfigCommand(t, args...); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(ats.ConfigPath)
			if err != nil {
				t.Fatalf("chart-test job needs an ATS config: %v", err)
			}
			var got map[string]any
			if err := yaml.UnmarshalStrict(data, &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"skip-steps": []any{"smoke", "functional", "upgrade"}}
			if tc.legacy {
				want["smoke-tests-cluster-type"] = "kind"
				want["upgrade-tests-cluster-type"] = "kind"
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ATS config = %#v, want %#v", got, want)
			}
			workflows, err := os.ReadFile(".circleci/workflows.yml")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(workflows), "name: execute-chart-tests\n") {
				t.Fatal("starter must accompany the branch chart-test job")
			}
			if release := strings.Contains(string(workflows), "name: execute-chart-tests-release\n"); release != tc.release {
				t.Errorf("release chart-test job = %t, args = %v", release, tc.args)
			}

			// Once a team enables scenarios, regeneration must preserve its edits.
			const ownConfig = "# team-owned\napp-tests-deploy-namespace: example\nskip-steps: [upgrade]\n"
			if err := os.WriteFile(ats.ConfigPath, []byte(ownConfig), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := runATSConfigCommand(t, args...); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(ats.ConfigPath); err != nil || string(data) != ownConfig {
				t.Fatalf("regeneration changed the team's config: %q, %v", data, err)
			}
		})
	}
}

func Test_Run_ATSConfigPreservesExisting(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
	}{
		{name: "case 0: yaml", files: map[string]string{ats.ConfigPath: "# custom\nskip-steps: [upgrade]\n"}},
		{name: "case 1: yml", files: map[string]string{".ats/main.yml": "# custom\nskip-steps: [upgrade]\n"}},
		{name: "case 2: empty yaml", files: map[string]string{ats.ConfigPath: ""}},
		{name: "case 3: both names", files: map[string]string{ats.ConfigPath: "# canonical\n", ".ats/main.yml": "# other\n"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.Mkdir(".ats", 0o750); err != nil {
				t.Fatal(err)
			}
			for name, content := range tc.files {
				if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := runATSConfigCommand(t, "--flavour", "app"); err != nil {
				t.Fatal(err)
			}
			for name, content := range tc.files {
				data, err := os.ReadFile(name) // #nosec G304 -- fixed test paths in a temporary directory
				if err != nil || string(data) != content {
					t.Errorf("existing %s changed: %q, %v", name, data, err)
				}
			}
			if _, existed := tc.files[ats.ConfigPath]; !existed {
				if _, err := os.Stat(ats.ConfigPath); !os.IsNotExist(err) {
					t.Fatalf("main.yml must not be shadowed by a new main.yaml: %v", err)
				}
			}
		})
	}
}

func Test_Run_ATSConfigOmittedWithoutJobs(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{name: "case 0: no app", args: []string{"--language", "go"}},
		{name: "case 1: explicit opt-out", args: []string{"--flavour", "app", "--skip-ats"}},
		{name: "case 2: template", args: []string{"--flavour", "app", "--component-type", "template", "--team", "team-example"}},
		{name: "case 3: fork", args: []string{"--flavour", "fork"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := runATSConfigCommand(t, tc.args...); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{".ats", "tests/ats"} {
				if _, err := os.Stat(name); !os.IsNotExist(err) {
					t.Errorf("unexpected ATS path %s: %v", name, err)
				}
			}
		})
	}
}

func Test_Run_ATSConfigProbeFailureBeforeWrites(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".ats", []byte("local obstruction\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runATSConfigCommand(t, "--flavour", "app"); err == nil {
		t.Fatal("expected an error when .ats is a file")
	}
	if _, err := os.Stat(filepath.Join(".circleci", "config.yml")); !os.IsNotExist(err) {
		t.Fatalf("failed config probe must leave CircleCI output absent: %v", err)
	}
	if data, err := os.ReadFile(".ats"); err != nil || string(data) != "local obstruction\n" {
		t.Fatalf("probe changed the obstruction: %q, %v", data, err)
	}
}
