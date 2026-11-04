package circleciconfig

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListUnknownWorkflowExpressions(t *testing.T) {
	for _, operator := range []string{"when", "unless"} {
		for _, expression := range []string{`pipeline.git.branch == "main"`, `pipeline.git.tag starts-with "v"`, `pipeline.parameters.enabled`, `1 > 2`, `not (1 == 2)`} {
			t.Run(operator+"/"+expression, func(t *testing.T) {
				config := fmt.Sprintf("version: 2.1\nworkflows:\n  always:\n    jobs: [build]\n  conditional:\n    %s: %q\n    jobs: [publish]\n", operator, expression)
				jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
				if err != nil || !found {
					t.Fatalf("found %v, err %v", found, err)
				}
				if diff := cmp.Diff([]string{"build"}, jobs.Names); diff != "" {
					t.Errorf("jobs (-want +got):\n%s", diff)
				}
				if !strings.Contains(strings.Join(jobs.Warnings, "\n"), "workflow conditional runs on a condition") {
					t.Errorf("missing condition warning: %v", jobs.Warnings)
				}
			})
		}
	}
}

func TestListUnknownJobFilterExpressions(t *testing.T) {
	for _, run := range []Run{{Branch: "feature"}, {Tag: "v1.2.3"}} {
		t.Run(run.String(), func(t *testing.T) {
			config := `version: 2.1
workflows:
  build:
    jobs:
    - build:
        filters:
          tags:
            only: /.*/
    - publish:
        filters: pipeline.git.branch == "main" or pipeline.git.tag starts-with "v"
    - downstream:
        requires: [publish]
        filters:
          tags:
            only: /.*/
`
			jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), run)
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if diff := cmp.Diff([]string{"build"}, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if !strings.Contains(strings.Join(jobs.Warnings, "\n"), "filters publish with an expression") {
				t.Errorf("missing expression warning: %v", jobs.Warnings)
			}
		})
	}
}

func TestListLegacyStringConditions(t *testing.T) {
	config := `version: 2.1
parameters:
  enabled:
    type: boolean
    default: true
workflows:
  literal:
    when: "a string"
    jobs: [literal]
  parameter:
    when: << pipeline.parameters.enabled >>
    jobs: [parameter]
  comparison:
    when:
      equal: [feature, << pipeline.git.branch >>]
    jobs: [comparison]
`
	jobs, found, err := List(context.Background(), files(map[string]string{ConfigPath: config}), Run{Branch: "feature"})
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if diff := cmp.Diff([]string{"comparison", "literal", "parameter"}, jobs.Names); diff != "" {
		t.Errorf("jobs (-want +got):\n%s", diff)
	}
	if len(jobs.Warnings) != 0 {
		t.Errorf("warnings: %v", jobs.Warnings)
	}
}
