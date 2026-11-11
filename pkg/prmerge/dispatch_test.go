package prmerge

import (
	"net/http"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

func Test_ParseDispatch(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Dispatch
		err  bool
	}{
		{in: "giantswarm/team-magazine/refresh.yaml", want: Dispatch{Owner: "giantswarm", Repo: "team-magazine", File: "refresh.yaml"}},
		{in: "o/r/refresh.yml@data", want: Dispatch{Owner: "o", Repo: "r", File: "refresh.yml", Ref: "data"}},
		{in: "", err: true},
		{in: "o/r", err: true},
		{in: "o/r/", err: true},
		{in: "/r/refresh.yaml", err: true},
		{in: "o/r/.github/workflows/refresh.yaml", err: true},
		{in: "o/r/refresh.yaml@", want: Dispatch{Owner: "o", Repo: "r", File: "refresh.yaml"}},
		{in: "o/r/refresh.yaml@refs/heads/main", err: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			d, err := ParseDispatch(tc.in)
			if tc.err {
				if err == nil || !strings.Contains(err.Error(), "<owner>/<repo>/<workflow file>[@<ref>]") {
					t.Fatalf("want the form in the error, got %v (%+v)", err, d)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if *d != tc.want {
				t.Errorf("want %+v, got %+v", tc.want, *d)
			}
			if d.String() != strings.TrimSuffix(tc.in, "@") {
				t.Errorf("String: want %q, got %q", strings.TrimSuffix(tc.in, "@"), d.String())
			}
		})
	}
}

// dispatchRoutes is the magazine repository x/y whose default branch is main
// and whose refresh.yaml accepts a dispatch.
func dispatchRoutes() sequence.Routes {
	return sequence.Routes{
		"GET /repos/x/y": {{Body: map[string]any{"full_name": "x/y", "default_branch": "main"}}},
		"POST /repos/x/y/actions/workflows/refresh.yaml/dispatches": {{Status: http.StatusNoContent}},
	}
}

func withDispatch(ref string) func(*Config) {
	return func(c *Config) { c.Dispatch = &Dispatch{Owner: "x", Repo: "y", File: "refresh.yaml", Ref: ref} }
}

// A merge dispatches the configured workflow on the repository's default
// branch with the merged pull request and its release as inputs, after the
// release wait.
func Test_Merge_dispatchesAfterTheRelease(t *testing.T) {
	var calls []releaseCall
	h := newHarness(t, routes(pull(nil), dispatchRoutes()), func(c *Config) {
		c.Release = fakeRelease(&calls, "v1.2.3", nil)
		withDispatch("")(c)
	})
	result, err := h.merge(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dispatch == nil || !result.Dispatch.Dispatched || result.Dispatch.Reason != "" {
		t.Fatalf("dispatch: %+v", result.Dispatch)
	}
	if result.Dispatch.Workflow != "x/y/refresh.yaml" || result.Dispatch.Ref != "main" {
		t.Errorf("dispatch: %+v", result.Dispatch)
	}
	for key, want := range map[string]string{InputRepository: "o/r", InputPullRequest: "42", InputRelease: "v1.2.3"} {
		if got := result.Dispatch.Inputs[key]; got != want {
			t.Errorf("input %s: want %q, got %q", key, want, got)
		}
	}
	if len(result.Dispatch.Inputs) != 3 {
		t.Errorf("want exactly the three declared inputs, got %v", result.Dispatch.Inputs)
	}
	if h.requested("POST /repos/x/y/actions/workflows/refresh.yaml/dispatches") != 1 {
		t.Errorf("want one dispatch, got %v", h.server.Requests())
	}
	requests := h.server.Requests()
	if last := requests[len(requests)-1]; last.Method != http.MethodPost {
		t.Errorf("want the dispatch after the release wait, last request %s", last)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("warnings: %v", result.Warnings)
	}
	if !strings.Contains(h.progress.String(), "dispatched: x/y/refresh.yaml on main with pull_request=42 release=v1.2.3 repository=o/r") {
		t.Errorf("progress:\n%s", h.progress.String())
	}
}

// A ref of its own is used as given, without reading the default branch.
func Test_Merge_dispatchesOnTheGivenRef(t *testing.T) {
	r := routes(pull(nil), dispatchRoutes())
	delete(r, "GET /repos/x/y")
	h := newHarness(t, r, withDispatch("data"))
	result, err := h.merge(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dispatch == nil || !result.Dispatch.Dispatched || result.Dispatch.Ref != "data" {
		t.Fatalf("dispatch: %+v", result.Dispatch)
	}
	if h.requested("GET /repos/x/y") != 0 {
		t.Errorf("the default branch was read for a given ref")
	}
}

// Without a release wait the dispatch follows the merge with an empty
// release; with one that found no release the input is empty too.
func Test_Merge_dispatchReleaseInput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*Config)
		want      string
	}{
		{name: "no release wait", configure: func(*Config) {}, want: ""},
		{name: "no release follows", configure: func(c *Config) {
			var calls []releaseCall
			c.Release = fakeRelease(&calls, "", &releasewait.NoReleaseError{Reason: "the Auto-release run finished without a tag"})
		}, want: ""},
		{name: "available", configure: func(c *Config) {
			var calls []releaseCall
			c.Release = fakeRelease(&calls, "v2.0.0", nil)
		}, want: "v2.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, routes(pull(nil), dispatchRoutes()), func(c *Config) { tc.configure(c); withDispatch("")(c) })
			result, err := h.merge(t)
			if err != nil {
				t.Fatal(err)
			}
			if result.Dispatch == nil || !result.Dispatch.Dispatched || result.Dispatch.Inputs[InputRelease] != tc.want {
				t.Errorf("dispatch: %+v", result.Dispatch)
			}
		})
	}
}

// The dispatch runs whatever the release's outcome: a merged pull request
// whose release failed still dispatches, and the exit code stays the
// release's.
func Test_Merge_dispatchesWhenTheReleaseFailed(t *testing.T) {
	var calls []releaseCall
	h := newHarness(t, routes(pull(nil), dispatchRoutes()), func(c *Config) {
		c.Release = fakeRelease(&calls, "v1.2.3", agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictCIFailed, "the tag pipeline of v1.2.3 failed"))
		withDispatch("")(c)
	})
	result, err := h.merge(t)
	if exitCode(err) != agentcli.ExitReleaseFailed {
		t.Fatalf("want exit 6, got %v", err)
	}
	if result.Dispatch == nil || !result.Dispatch.Dispatched || result.Dispatch.Inputs[InputRelease] != "v1.2.3" {
		t.Errorf("dispatch: %+v", result.Dispatch)
	}
}

// A dispatch GitHub refuses is a warning with the reason, in the document's
// dispatch too; the merge's outcome is unchanged.
func Test_Merge_dispatchFailureIsAWarning(t *testing.T) {
	refused := sequence.Routes{
		"GET /repos/x/y": {{Body: map[string]any{"full_name": "x/y", "default_branch": "main"}}},
		"POST /repos/x/y/actions/workflows/refresh.yaml/dispatches": {{Status: http.StatusUnprocessableEntity, Body: map[string]any{"message": "Unexpected inputs provided: [\"release\"]"}}},
	}
	h := newHarness(t, routes(pull(nil), refused), withDispatch(""))
	result, err := h.merge(t)
	if err != nil {
		t.Fatalf("a failed dispatch never fails the merge: %v", err)
	}
	if result.MergeCommitSHA != "m1" || !result.BranchDeleted {
		t.Errorf("result: %+v", result)
	}
	if result.Dispatch == nil || result.Dispatch.Dispatched || !strings.Contains(result.Dispatch.Reason, "Unexpected inputs provided") || !strings.Contains(result.Dispatch.Reason, "on main") {
		t.Fatalf("dispatch: %+v", result.Dispatch)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "the dispatch of x/y/refresh.yaml after the merge failed: ") || !strings.Contains(result.Warnings[0], "Unexpected inputs provided") {
		t.Errorf("warnings: %v", result.Warnings)
	}
	if !strings.Contains(h.progress.String(), "warning: the dispatch of x/y/refresh.yaml after the merge failed") {
		t.Errorf("progress:\n%s", h.progress.String())
	}
}

// A repository whose default branch cannot be read is the same warning,
// naming the read.
func Test_Merge_dispatchUnreadableRepository(t *testing.T) {
	missing := sequence.Routes{"GET /repos/x/y": {{Status: http.StatusNotFound, Body: map[string]any{"message": "Not Found"}}}}
	h := newHarness(t, routes(pull(nil), missing), withDispatch(""))
	result, err := h.merge(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dispatch == nil || result.Dispatch.Dispatched || !strings.Contains(result.Dispatch.Reason, "reading the default branch of x/y") {
		t.Errorf("dispatch: %+v", result.Dispatch)
	}
	if h.requested("POST /repos/x/y/actions/workflows/refresh.yaml/dispatches") != 0 {
		t.Errorf("dispatched without a ref")
	}
}

// Nothing merged, nothing dispatched: a refusal and a red head leave the
// dispatch null and send no dispatch.
func Test_Merge_nothingMergedDispatchesNothing(t *testing.T) {
	red := sequence.Routes{"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
		map[string]any{"id": 1, "name": "go-build", "status": "completed", "conclusion": "failure", "html_url": "https://github.com/o/r/runs/1"},
	}}}}}
	for _, tc := range []struct {
		name string
		pr   map[string]any
		more sequence.Routes
		code int
	}{
		{name: "another human", pr: pull(map[string]any{"user": map[string]any{"login": "alice", "type": "User"}}), code: agentcli.ExitRefused},
		{name: "red", pr: pull(nil), more: red, code: agentcli.ExitRed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, routes(tc.pr, dispatchRoutes(), tc.more), withDispatch(""))
			result, err := h.merge(t)
			if exitCode(err) != tc.code {
				t.Fatalf("want exit %d, got %v", tc.code, err)
			}
			if result.Dispatch != nil || h.requested("POST /repos/x/y/actions/workflows/refresh.yaml/dispatches") != 0 {
				t.Errorf("dispatched without a merge: %+v", result.Dispatch)
			}
		})
	}
}

// Without a dispatch configured nothing changes: no read of any other
// repository, dispatch null.
func Test_Merge_withoutDispatch(t *testing.T) {
	h := newHarness(t, routes(pull(nil)), nil)
	result, err := h.merge(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dispatch != nil {
		t.Errorf("dispatch: %+v", result.Dispatch)
	}
	for _, r := range h.server.Requests() {
		if strings.HasPrefix(r.Path, "/repos/x/") || strings.Contains(r.Path, "/dispatches") {
			t.Errorf("unexpected request %s", r)
		}
	}
}
