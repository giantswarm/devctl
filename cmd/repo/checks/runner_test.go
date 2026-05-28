package checks

import (
	"testing"

	"github.com/google/go-github/v88/github"
	"github.com/stretchr/testify/require"
)

func TestApplyChecks(t *testing.T) {
	existing := func(names ...string) []*github.RequiredStatusCheck {
		out := make([]*github.RequiredStatusCheck, 0, len(names))
		for _, n := range names {
			out = append(out, &github.RequiredStatusCheck{Context: n})
		}
		return out
	}

	tests := []struct {
		name     string
		existing []*github.RequiredStatusCheck
		add      []string
		remove   []string
		want     []string
	}{
		{
			name:     "add to empty",
			existing: nil,
			add:      []string{"a", "b"},
			want:     []string{"a", "b"},
		},
		{
			name:     "add deduplicates against existing",
			existing: existing("a"),
			add:      []string{"a", "b"},
			want:     []string{"a", "b"},
		},
		{
			name:     "remove drops named checks",
			existing: existing("a", "b", "c"),
			remove:   []string{"b"},
			want:     []string{"a", "c"},
		},
		{
			name:     "remove then add migrates a name",
			existing: existing("semantic-pull-request", "other"),
			add:      []string{"semantic-pull-request / Validate PR title"},
			remove:   []string{"semantic-pull-request"},
			want:     []string{"other", "semantic-pull-request / Validate PR title"},
		},
		{
			name:     "remove of absent name is a no-op",
			existing: existing("a"),
			remove:   []string{"b"},
			want:     []string{"a"},
		},
		{
			name:     "add deduplicates within --checks itself",
			existing: nil,
			add:      []string{"a", "a"},
			want:     []string{"a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyChecks(tc.existing, tc.add, tc.remove)
			names := make([]string, 0, len(got))
			for _, c := range got {
				names = append(names, c.GetContext())
			}
			require.Equal(t, tc.want, names)
		})
	}
}
