package circleciconfig

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListWorkflowNumericTruth(t *testing.T) {
	for _, tt := range []struct {
		literal string
		truth   bool
	}{
		{".nan", false}, {".NaN", false}, {"0.0", false}, {"-0.0", false}, {"0", false},
		{"1.5", true}, {"-1", true}, {".inf", true},
	} {
		for _, guard := range []string{"when", "unless"} {
			t.Run(guard+"/"+tt.literal, func(t *testing.T) {
				config := fmt.Sprintf("version: 2.1\nworkflows:\n  publish:\n    %s: %s\n    jobs: [publish]\n", guard, tt.literal)
				jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
				if err != nil || !found {
					t.Fatalf("found %v, err %v", found, err)
				}
				var want []string
				if (guard == "when") == tt.truth {
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
}
