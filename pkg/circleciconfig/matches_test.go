package circleciconfig

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListWorkflowMatchesWholeValue(t *testing.T) {
	tests := []struct {
		name, operator, pattern, branch string
		runs                            bool
	}{
		{"substring does not match", "when", "main", "feature-main", false},
		{"unless a substring", "unless", "main", "feature-main", true},
		{"whole string matches", "when", "main", "main", true},
		{"explicit prefix pattern", "when", "feature-.*", "feature-main", true},
		{"alternation cannot match a prefix", "when", "main|release", "main-feature", false},
		{"alternation cannot match a suffix", "when", "main|release", "feature-release", false},
		{"alternation matches a whole choice", "when", "main|release", "release", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := fmt.Sprintf(`version: 2.1
workflows:
  publish:
    %s:
      matches:
        pattern: %q
        value: << pipeline.git.branch >>
    jobs: [publish]
`, tt.operator, tt.pattern)
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: tt.branch})
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			var want []string
			if tt.runs {
				want = []string{"publish"}
			}
			if diff := cmp.Diff(want, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if len(jobs.Warnings) != 0 {
				t.Errorf("warnings: %v", jobs.Warnings)
			}
		})
	}
}
