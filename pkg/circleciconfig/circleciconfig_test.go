package circleciconfig

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// files reads from a map of path to content.
func files(m map[string]string) Reader {
	return func(_ context.Context, path string) ([]byte, bool, error) {
		content, ok := m[path]
		return []byte(content), ok, nil
	}
}

// generated are devctl's own generated configuration files.
func generated(t *testing.T) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, path := range []string{ConfigPath, WorkflowsPath} {
		content, err := fs.ReadFile(os.DirFS(filepath.Join("..", "..")), path)
		if err != nil {
			t.Fatal(err)
		}
		m[path] = string(content)
	}
	return m
}

// TestListGenerated: devctl's own generated configuration runs build-image
// behind go-build on a pull request's branch, and its release jobs on a tag
// only.
func TestListGenerated(t *testing.T) {
	tests := map[string]struct {
		run  Run
		want []string
	}{
		"a pull request's branch":  {run: Run{Branch: "feature"}, want: []string{"build-image", "go-build", "setup"}},
		"main":                     {run: Run{Branch: "main"}, want: []string{"go-build", "setup"}},
		"a release tag":            {run: Run{Tag: "v1.2.3"}, want: []string{"go-build", "push-to-registries-release", "setup", "sync-china-registry", "upload-release-assets"}},
		"a tag that is no release": {run: Run{Tag: "nightly"}, want: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			jobs, found, err := List(context.Background(), files(generated(t)), tt.run)
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if diff := cmp.Diff(tt.want, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if len(jobs.Warnings) > 0 {
				t.Errorf("warnings: %v", jobs.Warnings)
			}
		})
	}
}

func TestList(t *testing.T) {
	setup := "version: 2.1\nsetup: true\njobs:\n  setup:\n    steps:\n    - run: cp .circleci/workflows.yml .circleci/continue.yml\nworkflows:\n  setup:\n    jobs: [setup]\n"
	tests := map[string]struct {
		files map[string]string
		run   Run
		want  []string
		warn  string
	}{
		"no configuration": {files: map[string]string{}, run: Run{Branch: "feature"}},
		"custom.yml appends its jobs to the generated workflow": {
			files: map[string]string{
				ConfigPath:    setup,
				WorkflowsPath: "workflows:\n  build:\n    jobs:\n    - go-build\n",
				CustomPath:    "workflows:\n  build:\n    jobs:\n    - e2e:\n        requires: [go-build]\n",
			},
			run:  Run{Branch: "feature"},
			want: []string{"e2e", "go-build", "setup"},
		},
		"a job behind one filtered out never runs": {
			files: map[string]string{ConfigPath: `version: 2.1
workflows:
  build:
    jobs:
    - lint:
        filters:
          branches:
            only: main
    - test:
        requires: [lint]
    - build
`},
			run:  Run{Branch: "feature"},
			want: []string{"build"},
		},
		"an approval posts nothing, the job it holds does": {
			files: map[string]string{ConfigPath: `version: 2.1
workflows:
  deploy:
    jobs:
    - hold:
        type: approval
    - deploy:
        requires: [hold]
`},
			run:  Run{Branch: "feature"},
			want: []string{"deploy"},
		},
		"a scheduled workflow and a parameter's default": {
			files: map[string]string{ConfigPath: `version: 2.1
parameters:
  e2e:
    type: boolean
    default: false
workflows:
  nightly:
    triggers:
    - schedule:
        cron: "0 0 * * *"
    jobs: [nightly]
  e2e:
    when: << pipeline.parameters.e2e >>
    jobs: [e2e]
  push:
    unless:
      equal: [scheduled_pipeline, << pipeline.trigger_source >>]
    jobs: [test]
`},
			run:  Run{Branch: "feature"},
			want: []string{"test"},
		},
		"a condition only the pipeline knows": {
			files: map[string]string{ConfigPath: "version: 2.1\nworkflows:\n  build:\n    when: << pipeline.git.revision >>\n    jobs: [build]\n"},
			run:   Run{Branch: "feature"},
			warn:  "workflow build runs on a condition known only once the pipeline runs",
		},
		"a setup workflow devctl does not generate": {
			files: map[string]string{ConfigPath: "version: 2.1\nsetup: true\nworkflows:\n  setup:\n    jobs: [path-filter]\n"},
			run:   Run{Branch: "feature"},
			want:  []string{"path-filter"},
			warn:  "continues with a configuration devctl does not generate",
		},
		"a matrix job": {
			files: map[string]string{ConfigPath: "version: 2.1\nworkflows:\n  build:\n    jobs:\n    - test:\n        matrix:\n          parameters:\n            go: [\"1.25\", \"1.26\"]\n"},
			run:   Run{Branch: "feature"},
			warn:  "runs test as a matrix",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			jobs, _, err := List(context.Background(), files(tt.files), tt.run)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			warnings := strings.Join(jobs.Warnings, "\n")
			if tt.warn == "" && warnings != "" || !strings.Contains(warnings, tt.warn) {
				t.Errorf("warnings %q, want %q", warnings, tt.warn)
			}
		})
	}
}
