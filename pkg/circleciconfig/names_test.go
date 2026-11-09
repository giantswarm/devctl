package circleciconfig

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListDuplicateJobNames(t *testing.T) {
	for name, entries := range map[string]string{
		"implicit names":                       "    - test\n    - test\n",
		"explicit names":                       "    - test:\n        name: check\n    - test:\n        name: check\n",
		"different jobs same explicit name":    "    - test:\n        name: check\n    - lint:\n        name: check\n",
		"explicit name collides with implicit": "    - test\n    - lint:\n        name: test\n",
	} {
		t.Run(name, func(t *testing.T) {
			config := "version: 2.1\nworkflows:\n  build:\n    jobs:\n    - build\n" + entries
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if diff := cmp.Diff([]string{"build"}, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if len(jobs.Warnings) != 1 || !strings.Contains(jobs.Warnings[0], "CircleCI assigns job name suffixes") {
				t.Errorf("want one naming warning, got %v", jobs.Warnings)
			}
		})
	}
}

func TestListDistinctInvocationNames(t *testing.T) {
	config := `version: 2.1
workflows:
  build:
    jobs:
    - test:
        name: test-linux
    - test:
        name: test-macos
    - finish:
        requires: [test-linux, test-macos]
  independent:
    jobs: [test-linux]
`
	jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if diff := cmp.Diff([]string{"finish", "test-linux", "test-macos"}, jobs.Names); diff != "" {
		t.Errorf("jobs (-want +got):\n%s", diff)
	}
	if len(jobs.Warnings) != 0 {
		t.Errorf("warnings: %v", jobs.Warnings)
	}
}
