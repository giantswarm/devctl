package circleciconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListRuntimeContinuation(t *testing.T) {
	multi := map[string]string{}
	for path, fixture := range map[string]string{ConfigPath: "agent-toolchains.config.yml", WorkflowsPath: "agent-toolchains.workflows.yml"} {
		content, err := os.ReadFile(filepath.Join("..", "gen", "input", "circleci", "testdata", fixture)) // #nosec G304 -- fixed test fixture names
		if err != nil {
			t.Fatal(err)
		}
		multi[path] = string(content)
	}
	for _, run := range []Run{{Branch: "feature"}, {Tag: "a/v1.2.3"}} {
		t.Run(run.String(), func(t *testing.T) {
			var reads []string
			read := func(ctx context.Context, path string) ([]byte, bool, error) {
				reads = append(reads, path)
				return files(multi)(ctx, path)
			}
			jobs, found, err := List(context.Background(), read, run)
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if diff := cmp.Diff([]string{"setup"}, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if !strings.Contains(strings.Join(jobs.Warnings, "\n"), "continued jobs count once they post a status") {
				t.Errorf("runtime continuation needs an unknown-jobs warning: %v", jobs.Warnings)
			}
			if diff := cmp.Diff([]string{ConfigPath}, reads); diff != "" {
				t.Errorf("reads (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListUnrelatedContinuationPathMention(t *testing.T) {
	m := map[string]string{
		ConfigPath: `version: 2.1
setup: true
# This setup does not continue with .circleci/workflows.yml.
workflows:
  setup:
    jobs: [path-filter]
`,
		WorkflowsPath: "[invalid unused configuration",
	}
	jobs, found, err := List(context.Background(), files(m), Run{Branch: "feature"})
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if diff := cmp.Diff([]string{"path-filter"}, jobs.Names); diff != "" {
		t.Errorf("jobs (-want +got):\n%s", diff)
	}
	if len(jobs.Warnings) != 1 {
		t.Errorf("want unknown continuation warning, got %v", jobs.Warnings)
	}
}

func TestListModifiedStaticContinuation(t *testing.T) {
	original := generated(t)[ConfigPath]
	for name, config := range map[string]string{
		"different output path":      strings.ReplaceAll(original, ".circleci/continue.yml", ".circleci/other.yml"),
		"additional transformation":  strings.Replace(original, "    - continuation/continue:", "    - run: sed -i 's/build/test/g' .circleci/continue.yml\n    - continuation/continue:", 1),
		"different continuation orb": strings.Replace(original, "circleci/continuation@", "example/continuation@", 1),
	} {
		t.Run(name, func(t *testing.T) {
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config, WorkflowsPath: "[unused malformed YAML"}), Run{Branch: "feature"})
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if diff := cmp.Diff([]string{"setup"}, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if len(jobs.Warnings) != 1 {
				t.Errorf("want unknown continuation warning, got %v", jobs.Warnings)
			}
		})
	}
}
