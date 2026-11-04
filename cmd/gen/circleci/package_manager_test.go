package circleci

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"sigs.k8s.io/yaml"
)

func Test_Run_InvalidPackageManagerLeavesFilesUntouched(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".circleci", 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"package-lock.json":       "{}\n",
		".circleci/config.yml":    "previous setup\n",
		".circleci/workflows.yml": "previous workflows\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--repo-name=fixture", "--language=node", "--package-manager=nmp"})
	err = cmd.Execute()
	if !IsInvalidFlag(err) || !strings.Contains(err.Error(), "--package-manager") || !strings.Contains(err.Error(), "nmp") {
		t.Errorf("unsupported package manager: got %v, want invalid flag naming the value", err)
	}
	for path, want := range files {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("%s changed after invalid package manager: err=%v", path, err)
		}
	}
}

// Check both sides of the enum boundary through the command: accepted values
// must retain their toolchain, and an omitted value must still use repo content.
func Test_Run_PackageManagerToolchains(t *testing.T) {
	for _, tc := range []struct {
		name, manager, lockfile, content, install, cacheKey string
	}{
		{"explicit npm", "npm", "yarn.lock", "__metadata:\n", "npm ci", `node-deps-npm-v1-{{ checksum "package-lock.json" }}`},
		{"explicit pnpm", "pnpm", "package-lock.json", "{}\n", "pnpm install --frozen-lockfile", `node-deps-pnpm-v1-{{ checksum "pnpm-lock.yaml" }}`},
		{"explicit Yarn", "yarn", "package-lock.json", "{}\n", "yarn install --immutable", `node-deps-yarn-v1-{{ checksum "yarn.lock" }}`},
		{"explicit classic Yarn", "yarn-classic", "package-lock.json", "{}\n", "yarn install --frozen-lockfile", `node-deps-yarn-classic-v1-{{ checksum "yarn.lock" }}`},
		{"detect npm", "", "package-lock.json", "{}\n", "npm ci", `node-deps-npm-v1-{{ checksum "package-lock.json" }}`},
		{"detect pnpm", "", "pnpm-lock.yaml", "lockfileVersion: '9.0'\n", "pnpm install --frozen-lockfile", `node-deps-pnpm-v1-{{ checksum "pnpm-lock.yaml" }}`},
		{"detect Yarn", "", "yarn.lock", "__metadata:\n", "yarn install --immutable", `node-deps-yarn-v1-{{ checksum "yarn.lock" }}`},
		{"detect classic Yarn", "", "yarn.lock", "# yarn lockfile v1\n", "yarn install --frozen-lockfile", `node-deps-yarn-classic-v1-{{ checksum "yarn.lock" }}`},
		{"default Yarn", "", "", "", "yarn install --immutable", `node-deps-yarn-v1-{{ checksum "yarn.lock" }}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.lockfile != "" {
				if err := os.WriteFile(tc.lockfile, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			args := []string{"--repo-name=fixture", "--language=node"}
			if tc.manager != "" {
				args = append(args, "--package-manager="+tc.manager)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(".circleci/workflows.yml")
			if err != nil {
				t.Fatal(err)
			}
			var pipeline struct {
				Jobs map[string]struct {
					Steps []any `json:"steps"`
				} `json:"jobs"`
			}
			if err := yaml.Unmarshal(data, &pipeline); err != nil {
				t.Fatal(err)
			}
			var install, cacheKey string
			for _, raw := range pipeline.Jobs["node-test"].Steps {
				step, ok := raw.(map[string]any)
				if !ok {
					continue // checkout is a scalar step.
				}
				run, _ := step["run"].(map[string]any)
				if run["name"] == "Install dependencies" {
					install, _ = run["command"].(string)
				}
				cache, _ := step["save_cache"].(map[string]any)
				if key, ok := cache["key"].(string); ok && strings.HasPrefix(key, "node-deps-") {
					cacheKey = key
				}
			}
			if install != tc.install || cacheKey != tc.cacheKey {
				t.Errorf("got install %q and cache %q; want %q and %q", install, cacheKey, tc.install, tc.cacheKey)
			}
		})
	}
}

func Test_Run_PackageManagerIgnoredWithoutNodeJob(t *testing.T) {
	for _, args := range [][]string{
		{"--language=go"},
		{"--language=node", "--flavour=multi-image"},
		{"--language=node", "--flavour=fork"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Chdir(t.TempDir())
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append([]string{"--repo-name=fixture", "--package-manager=unsupported"}, args...))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("package-manager applies only to a Node job: %v", err)
			}
		})
	}
}
