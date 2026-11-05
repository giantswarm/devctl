package pr

import "testing"

func TestGroupRenovatePRsSearchesNormalizedWords(t *testing.T) {
	testCases := []struct {
		name  string
		title string
		query string
	}{
		{
			name:  "case 0: connective word removed from group label",
			title: "Update build and test dependencies",
			query: `"build" "test" "dependencies" in:title`,
		},
		{
			name:  "case 1: conventional prefix retained but update skipped",
			title: "fix(deps): update monitoring stack",
			query: `"fix(deps):" "monitoring" "stack" in:title`,
		},
		{
			name:  "case 2: version removed between group words",
			title: "Update Go v1.26.0 toolchain",
			query: `"Go" "toolchain" in:title`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			groups := GroupRenovatePRs([]*PRInfo{{Title: tc.title}})
			if len(groups) != 1 {
				t.Fatalf("got %d groups, want 1", len(groups))
			}
			// GitHub treats a quoted multiword term as a phrase. Words
			// made adjacent by normalization must instead be separate terms
			// so the source title still matches the generated query.
			if groups[0].SearchQuery != tc.query {
				t.Errorf("query for title %q = %q, want %q", tc.title, groups[0].SearchQuery, tc.query)
			}
		})
	}
}
