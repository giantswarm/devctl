package circleciconfig

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListFilteredDependencies(t *testing.T) {
	tests := []struct {
		name         string
		dependencies string
		want         []string
	}{
		{"one prerequisite remains", "[build, main-only]", []string{"build", "deploy", "finish"}},
		{"all prerequisites filtered", "[main-only, child-of-main]", []string{"build"}},
		{"transitively filtered prerequisite ignored", "[child-of-main, build]", []string{"build", "deploy", "finish"}},
		{"status-qualified prerequisites", "[build, {main-only: [success, failed]}]", []string{"build", "deploy", "finish"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := fmt.Sprintf(`version: 2.1
workflows:
  build:
    jobs:
    - main-only:
        filters:
          branches:
            only: main
    - child-of-main:
        requires: [main-only]
    - build
    - deploy:
        requires: %s
    - hold:
        type: approval
        requires: [deploy]
    - finish:
        requires: [hold, deploy]
`, tt.dependencies)
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if diff := cmp.Diff(tt.want, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if len(jobs.Warnings) != 0 {
				t.Errorf("warnings: %v", jobs.Warnings)
			}
		})
	}
}
