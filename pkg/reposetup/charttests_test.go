package reposetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test_writeChartTests pins when the scaffold gets the chart tests: after
// the generators emitted the ATS dependency file, and only where the
// template carries neither app-test-suite's configuration nor a test of its
// own.
func Test_writeChartTests(t *testing.T) {
	const (
		name       = "example-go-service"
		ownConfig  = "app-tests-deploy-namespace: example\n"
		ownTest    = "def test_own() -> None:\n    pass\n"
		pyproject  = "[project]\nname = \"ats-tests\"\n"
		nestedTest = "tests/ats/checks/test_nested.py"
	)

	cases := map[string]struct {
		// files are the scaffold's files before the write.
		files map[string]string
		// wantConfig is the expected content of .ats/main.yaml, "" for
		// the file not to exist.
		wantConfig string
		// wantSmoke says whether the scaffold's smoke test is written.
		wantSmoke bool
	}{
		"no dependency file: nothing": {
			files: map[string]string{"helm/example/Chart.yaml": "name: example\n"},
		},
		"dependency file alone: both": {
			files:      map[string]string{atsDependenciesPath: pyproject},
			wantConfig: scaffoldATSConfig(t),
			wantSmoke:  true,
		},
		"template carries the configuration: kept": {
			files:      map[string]string{atsDependenciesPath: pyproject, atsConfigPath: ownConfig},
			wantConfig: ownConfig,
			wantSmoke:  true,
		},
		"template carries a test: no smoke": {
			files:      map[string]string{atsDependenciesPath: pyproject, "tests/ats/test_basic.py": ownTest},
			wantConfig: scaffoldATSConfig(t),
		},
		"template carries a nested test: no smoke": {
			files:      map[string]string{atsDependenciesPath: pyproject, nestedTest: ownTest},
			wantConfig: scaffoldATSConfig(t),
		},
	}

	for tn, tc := range cases {
		t.Run(tn, func(t *testing.T) {
			dir := t.TempDir()
			for p, content := range tc.files {
				if err := writeFile(filepath.Join(dir, filepath.FromSlash(p)), []byte(content), fileMode); err != nil {
					t.Fatal(err)
				}
			}

			if err := writeChartTests(dir, name); err != nil {
				t.Fatalf("writeChartTests: %v", err)
			}

			config, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(atsConfigPath))) // #nosec G304 -- a fixed path under t.TempDir()
			switch {
			case tc.wantConfig == "" && !os.IsNotExist(err):
				t.Errorf("%s: got %q, %v; want the file absent", atsConfigPath, config, err)
			case tc.wantConfig != "" && err != nil:
				t.Errorf("%s: %v", atsConfigPath, err)
			case tc.wantConfig != "" && string(config) != tc.wantConfig:
				t.Errorf("%s:\n%s\nwant:\n%s", atsConfigPath, config, tc.wantConfig)
			}

			smoke, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(atsSmokeTestPath))) // #nosec G304 -- a fixed path under t.TempDir()
			if !tc.wantSmoke {
				if !os.IsNotExist(err) {
					t.Errorf("%s: got %q, %v; want the file absent", atsSmokeTestPath, smoke, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", atsSmokeTestPath, err)
			}
			for _, want := range []string{"ATS smoke for the " + name + " chart", "@pytest.mark.smoke", "def test_api_working(kube_cluster: Cluster)"} {
				if !strings.Contains(string(smoke), want) {
					t.Errorf("%s lacks %q:\n%s", atsSmokeTestPath, want, smoke)
				}
			}
			if strings.Contains(string(smoke), appNamePlaceholder) {
				t.Errorf("%s keeps the placeholder %s:\n%s", atsSmokeTestPath, appNamePlaceholder, smoke)
			}
		})
	}
}

// scaffoldATSConfig is the embedded .ats/main.yaml as the scaffold writes
// it: no placeholder, the two scenarios a new repository cannot run skipped.
func scaffoldATSConfig(t *testing.T) string {
	t.Helper()
	data, err := chartTestFiles.ReadFile("scaffold/ats/main.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "skip-steps: [functional, upgrade]") {
		t.Fatalf("embedded .ats/main.yaml does not skip the functional and upgrade scenarios:\n%s", data)
	}
	return string(data)
}
