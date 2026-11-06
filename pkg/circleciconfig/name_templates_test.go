package circleciconfig

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListTemplatedJobNames(t *testing.T) {
	for _, name := range []string{"test-<< pipeline.parameters.target >>", "<< pipeline.parameters.target >>", "test-<< pipeline.git.revision >>"} {
		t.Run(name, func(t *testing.T) {
			config := fmt.Sprintf(`version: 2.1
parameters:
  target:
    type: string
    default: linux
workflows:
  build:
    jobs:
    - build
    - test:
        name: %q
    - finish:
        requires: [%q]
`, name, name)
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if diff := cmp.Diff([]string{"build"}, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if len(jobs.Warnings) != 1 || !strings.Contains(jobs.Warnings[0], "a name using a template") {
				t.Errorf("want template warning, got %v", jobs.Warnings)
			}
		})
	}
}

func TestListLiteralJobNames(t *testing.T) {
	config := `version: 2.1
workflows:
  build:
    jobs:
    - test:
        name: test-linux
    - finish:
        requires: [test-linux]
`
	jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if diff := cmp.Diff([]string{"finish", "test-linux"}, jobs.Names); diff != "" {
		t.Errorf("jobs (-want +got):\n%s", diff)
	}
	if len(jobs.Warnings) != 0 {
		t.Errorf("warnings: %v", jobs.Warnings)
	}
}
