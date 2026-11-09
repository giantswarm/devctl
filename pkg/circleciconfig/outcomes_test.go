package circleciconfig

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListDependencyOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name, requirement string
		expected          bool
	}{
		{"ordinary success", "[build]", true},
		{"explicit success", "[{build: success}]", true},
		{"success or failure", "[{build: [success, failed]}]", true},
		{"all terminal outcomes", "[{build: terminal}]", true},
		{"failure only", "[{build: failed}]", false},
		{"cancellation only", "[{build: canceled}]", false},
		{"not run only", "[{build: not_run}]", false},
		{"multiple unsuccessful outcomes", "[{build: [failed, canceled]}]", false},
		{"one failure-only prerequisite", "[{build: success}, {other: failed}]", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := fmt.Sprintf(`version: 2.1
workflows:
  build:
    jobs:
    - build
    - other
    - cleanup:
        requires: %s
    - finish:
        requires: [cleanup]
`, tt.requirement)
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			want := []string{"build", "other"}
			if tt.expected {
				want = []string{"build", "cleanup", "finish", "other"}
			}
			if diff := cmp.Diff(want, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if tt.expected {
				if len(jobs.Warnings) != 0 {
					t.Errorf("warnings: %v", jobs.Warnings)
				}
			} else if len(jobs.Warnings) != 1 || !strings.Contains(jobs.Warnings[0], "particular dependency outcomes") {
				t.Errorf("want outcome warning, got %v", jobs.Warnings)
			}
		})
	}
}
