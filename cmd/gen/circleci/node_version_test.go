package circleci

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"sigs.k8s.io/yaml"

	circleciinput "github.com/giantswarm/devctl/v8/pkg/gen/input/circleci"
)

// Exercise the command's precedence and warning boundary, which neither the
// .nvmrc parser tests nor the input package's direct Config tests execute.
func Test_Run_NodeVersionSelection(t *testing.T) {
	for _, tc := range []struct {
		name, nvmrc, override, version, rejected string
		goRepo                                   bool
	}{
		{name: "absent file", version: circleciinput.DefaultNodeImageVersion},
		{name: "repo version", nvmrc: "v99.0.0 # fixture\n", version: "99.0.0"},
		{name: "alias warns and falls back", nvmrc: "lts/*\n", version: circleciinput.DefaultNodeImageVersion, rejected: "lts/*"},
		{name: "partial version warns and falls back", nvmrc: "99.0\n", version: circleciinput.DefaultNodeImageVersion, rejected: "99.0"},
		{name: "explicit overrides repo", nvmrc: "99.0.0\n", override: "99.1.2", version: "99.1.2"},
		{name: "explicit bypasses rejected repo value", nvmrc: "lts/*\n", override: "99.1.2", version: "99.1.2"},
		{name: "non-Node ignores file", nvmrc: "lts/*\n", goRepo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.nvmrc != "" {
				if err := os.WriteFile(".nvmrc", []byte(tc.nvmrc), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			var warnings bytes.Buffer
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: &warnings})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			language := "node"
			if tc.goRepo {
				language = "go"
			}
			args := []string{"--repo-name=fixture", "--language=" + language}
			if tc.override != "" {
				args = append(args, "--node-image-version="+tc.override)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if tc.rejected == "" {
				if warnings.Len() != 0 {
					t.Errorf("unexpected warning: %s", warnings.String())
				}
			} else {
				for _, want := range []string{"warning: ignoring .nvmrc", tc.rejected, "exact major.minor.patch", circleciinput.DefaultNodeImageVersion} {
					if !strings.Contains(warnings.String(), want) {
						t.Errorf("warning %q does not include %q", warnings.String(), want)
					}
				}
			}
			data, err := os.ReadFile(".circleci/workflows.yml")
			if err != nil {
				t.Fatal(err)
			}
			var pipeline struct {
				Jobs map[string]struct {
					Docker []struct {
						Image string `json:"image"`
					} `json:"docker"`
					Steps []any `json:"steps"`
				} `json:"jobs"`
			}
			if err := yaml.Unmarshal(data, &pipeline); err != nil {
				t.Fatal(err)
			}
			job, hasNodeJob := pipeline.Jobs["node-test"]
			if tc.goRepo {
				if hasNodeJob || len(pipeline.Jobs) != 0 {
					t.Error("non-Node generation emitted a Node job")
				}
				return
			}
			if !hasNodeJob || len(job.Docker) != 1 || job.Docker[0].Image != "cimg/node:"+tc.version {
				t.Fatalf("executor images = %v; want cimg/node:%s", job.Docker, tc.version)
			}
			wantKey := "node-build-yarn-v2-" + tc.version + `-{{ checksum "yarn.lock" }}`
			var restores, saves int
			for _, raw := range job.Steps {
				step, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if restore, ok := step["restore_cache"].(map[string]any); ok {
					keys, _ := restore["keys"].([]any)
					for _, key := range keys {
						if key == wantKey {
							restores++
							if len(keys) != 1 {
								t.Error("build cache restore must use only the exact versioned key")
							}
						}
					}
				}
				if save, ok := step["save_cache"].(map[string]any); ok && save["key"] == wantKey {
					saves++
				}
			}
			if restores != 1 || saves != 1 {
				t.Errorf("cache key %q: %d restores, %d saves; want one of each", wantKey, restores, saves)
			}
		})
	}
}
