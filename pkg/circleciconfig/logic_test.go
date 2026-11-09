package circleciconfig

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListPartlyKnownLogic(t *testing.T) {
	for _, tt := range []struct {
		name, guard, logic string
		runs, warning      bool
	}{
		{"or decided before unknown", "when", "{or: [true, << pipeline.git.revision >>]}", true, false},
		{"or decided after unknown", "when", "{or: [<< pipeline.git.revision >>, true]}", true, false},
		{"and decided before unknown", "when", "{and: [false, << pipeline.git.revision >>]}", false, false},
		{"and decided after unknown", "when", "{and: [<< pipeline.git.revision >>, false]}", false, false},
		{"unless known true", "unless", "{or: [<< pipeline.git.revision >>, true]}", false, false},
		{"unless known false", "unless", "{and: [<< pipeline.git.revision >>, false]}", true, false},
		{"nested known result", "when", "{not: {and: [<< pipeline.git.revision >>, false]}}", true, false},
		{"or still unknown", "when", "{or: [false, << pipeline.git.revision >>]}", false, true},
		{"and still unknown", "when", "{and: [true, << pipeline.git.revision >>]}", false, true},
		{"known true and", "when", "{and: [true, true]}", true, false},
		{"known false or", "when", "{or: [false, false]}", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := fmt.Sprintf("version: 2.1\nworkflows:\n  publish:\n    %s: %s\n    jobs: [publish]\n", tt.guard, tt.logic)
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
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
			if (len(jobs.Warnings) != 0) != tt.warning {
				t.Errorf("warnings %v, want warning %v", jobs.Warnings, tt.warning)
			}
		})
	}
}
