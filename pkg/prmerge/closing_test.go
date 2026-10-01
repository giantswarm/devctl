package prmerge

import (
	"net/http"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func Test_closingReferences(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []reference
	}{
		{"same repository", "Fixes #7.", []reference{{phrase: "Fixes #7", number: 7}}},
		{"colon and past tense", "closed: #8", []reference{{phrase: "closed: #8", number: 8}}},
		{"possessive", "fixes #7's flake", []reference{{phrase: "fixes #7", number: 7}}},
		{"GH form", "Resolves GH-9", []reference{{phrase: "Resolves GH-9", number: 9}}},
		{"another repository", "closes other/repo#4", []reference{{phrase: "closes other/repo#4", owner: "other", repo: "repo", number: 4}}},
		{"issue URL", "Resolved https://github.com/other/repo.go/issues/12", []reference{{phrase: "Resolved https://github.com/other/repo.go/issues/12", owner: "other", repo: "repo.go", number: 12}}},
		{"pull URL", "FIX https://github.com/o/r/pull/5", []reference{{phrase: "FIX https://github.com/o/r/pull/5", owner: "o", repo: "r", number: 5, pull: true}}},
		{"one keyword per reference", "Fixes #1, fixes #2 and #3", []reference{{phrase: "Fixes #1", number: 1}, {phrase: "fixes #2", number: 2}}},
		{"no keyword", "Refs #7, see other/repo#4", nil},
		{"a word containing a keyword", "prefix #3, hotfix #3, suffixes #3", nil},
		{"not a number", "fixes #7abc", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := closingReferences(tc.body)
			if len(got) != len(tc.want) {
				t.Fatalf("closingReferences(%q) = %+v, want %+v", tc.body, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("reference %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Test_Merge_warnsOnClosingKeywords: a closing keyword before a pull request
// or an item of another repository is a warning in the document and on the
// progress stream before the merge; before an issue of the repository, a
// number that does not exist or no keyword at all, nothing. The merge
// proceeds either way.
func Test_Merge_warnsOnClosingKeywords(t *testing.T) {
	body := strings.Join([]string{
		// #7 is a pull request.
		"Fixes #7's flake.",
		// An issue of o/r.
		"Closes #8.",
		// The same issue, the repository spelled in capitals.
		"Fixes O/R#8 too.",
		// Does not exist.
		"Resolves: #9.",
		// Another repository.
		"closes other/repo#4.",
		// A pull request by its URL.
		"Resolves https://github.com/o/r/pull/5.",
		// No closing keyword.
		"Refs #7, prefix #7.",
		// Cannot be read.
		"fixed #10.",
	}, "\n")
	h := newHarness(t, routes(pull(map[string]any{"body": body}), sequence.Routes{
		"GET /repos/o/r/issues/7":  {{Body: map[string]any{"number": 7, "pull_request": map[string]any{"url": "https://api.github.com/repos/o/r/pulls/7"}}}},
		"GET /repos/o/r/issues/8":  {{Body: map[string]any{"number": 8}}},
		"GET /repos/o/r/issues/9":  {{Status: http.StatusNotFound, Body: map[string]any{"message": "Not Found"}}},
		"GET /repos/o/r/issues/10": {{Status: http.StatusForbidden, Body: map[string]any{"message": "Resource not accessible by integration"}}},
	}), nil)

	result, err := h.merge(t)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if h.requested("PUT /repos/o/r/pulls/42/merge") != 1 {
		t.Fatal("the merge was not made: the warnings must not stop it")
	}
	want := []string{
		`the body's "Fixes #7" closes pull request #7, unmerged`,
		`the body's "closes other/repo#4" closes other/repo#4, in another repository`,
		`the body's "Resolves https://github.com/o/r/pull/5" closes pull request #5, unmerged`,
		`the body's "fixed #10" closes #10 when this pull request merges, and whether #10 is a pull request could not be read`,
	}
	if len(result.Warnings) != len(want) {
		t.Fatalf("warnings = %q, want %d", result.Warnings, len(want))
	}
	for i, w := range want {
		if !strings.Contains(result.Warnings[i], w) {
			t.Errorf("warning %d = %q, want it to contain %q", i, result.Warnings[i], w)
		}
		if !strings.Contains(h.progress.String(), "warning: "+result.Warnings[i]) {
			t.Errorf("warning %d is not on the progress stream: %q", i, h.progress.String())
		}
	}
	if n := h.requested("GET /repos/o/r/issues/5") + h.requested("GET /repos/other/repo/issues/4"); n != 0 {
		t.Errorf("a pull URL and another repository's reference need no lookup, got %d", n)
	}
}
