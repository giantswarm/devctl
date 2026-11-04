package circleciconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListReaderOutcomes(t *testing.T) {
	tests := []struct {
		name, absent, failed, invalid string
		wantNames                     []string
		wantFound                     bool
		wantReads                     []string
		warning                       bool
	}{
		{name: "missing config", absent: ConfigPath, wantReads: []string{ConfigPath}},
		{name: "config read failure", failed: ConfigPath, wantReads: []string{ConfigPath}},
		{name: "invalid config", invalid: ConfigPath, wantFound: true, wantReads: []string{ConfigPath}},
		{name: "missing workflows", absent: WorkflowsPath, wantFound: true, wantNames: []string{"setup"}, warning: true, wantReads: []string{ConfigPath, WorkflowsPath}},
		{name: "workflows read failure", failed: WorkflowsPath, wantFound: true, wantReads: []string{ConfigPath, WorkflowsPath}},
		{name: "invalid workflows", invalid: WorkflowsPath, wantFound: true, wantReads: []string{ConfigPath, WorkflowsPath}},
		{name: "missing custom", absent: CustomPath, wantFound: true, wantNames: []string{"build", "setup"}, wantReads: []string{ConfigPath, WorkflowsPath, CustomPath}},
		{name: "custom read failure", failed: CustomPath, wantFound: true, wantReads: []string{ConfigPath, WorkflowsPath, CustomPath}},
		{name: "invalid custom", invalid: CustomPath, wantFound: true, wantReads: []string{ConfigPath, WorkflowsPath, CustomPath}},
		{name: "all present", wantFound: true, wantNames: []string{"build", "e2e", "setup"}, wantReads: []string{ConfigPath, WorkflowsPath, CustomPath}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := readerFixture(t)
			if tt.absent != "" {
				delete(m, tt.absent)
			}
			if tt.invalid != "" {
				m[tt.invalid] = "workflows: [unclosed"
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("synthetic reader failure")
			var reads []string
			read := func(got context.Context, path string) ([]byte, bool, error) {
				if got != ctx {
					t.Error("Reader did not receive the caller's context")
				}
				reads = append(reads, path)
				if path == tt.failed {
					return nil, false, failure
				}
				return files(m)(got, path)
			}
			jobs, found, err := List(ctx, read, Run{Branch: "feature"})
			if found != tt.wantFound {
				t.Errorf("found %v, want %v", found, tt.wantFound)
			}
			switch {
			case tt.failed != "":
				if !errors.Is(err, failure) {
					t.Errorf("error %v does not preserve Reader error", err)
				}
			case tt.invalid != "":
				if err == nil || !strings.Contains(err.Error(), "parsing "+tt.invalid+":") {
					t.Errorf("want parse error naming %s, got %v", tt.invalid, err)
				}
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.wantNames, jobs.Names); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantReads, reads); diff != "" {
				t.Errorf("reads (-want +got):\n%s", diff)
			}
			if tt.warning {
				if len(jobs.Warnings) != 1 || !strings.Contains(jobs.Warnings[0], "continued jobs count once they post a status") {
					t.Errorf("want unknown-continuation warning, got %v", jobs.Warnings)
				}
			} else if len(jobs.Warnings) != 0 {
				t.Errorf("unexpected warnings: %v", jobs.Warnings)
			}
		})
	}
}

func TestListReaderCancellation(t *testing.T) {
	paths := []string{ConfigPath, WorkflowsPath, CustomPath}
	for index, failed := range paths {
		t.Run(failed, func(t *testing.T) {
			m := readerFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var reads []string
			read := func(got context.Context, path string) ([]byte, bool, error) {
				if got != ctx {
					t.Error("Reader did not receive the caller's context")
				}
				reads = append(reads, path)
				if path == failed {
					cancel()
					return nil, false, got.Err()
				}
				return files(m)(got, path)
			}
			jobs, found, err := List(ctx, read, Run{Branch: "feature"})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("want context.Canceled, got %v", err)
			}
			if found != (index > 0) {
				t.Errorf("found %v, want %v", found, index > 0)
			}
			if diff := cmp.Diff(Jobs{}, jobs); diff != "" {
				t.Errorf("partial result on error (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(paths[:index+1], reads); diff != "" {
				t.Errorf("reads after cancellation (-want +got):\n%s", diff)
			}
		})
	}
}

func readerFixture(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		ConfigPath:    generated(t)[ConfigPath],
		WorkflowsPath: "workflows:\n  build:\n    jobs: [build]\n",
		CustomPath:    "workflows:\n  build:\n    jobs:\n    - e2e:\n        requires: [build]\n",
	}
}
