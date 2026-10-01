package releasewait

import (
	"slices"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		given string
		bare  string
		tags  []string
		err   bool
	}{
		{given: "v1.2.3", bare: "1.2.3", tags: []string{"v1.2.3", "1.2.3"}},
		{given: "1.2.3", bare: "1.2.3", tags: []string{"1.2.3", "v1.2.3"}},
		{given: "v1.2.3-rc.1", bare: "1.2.3-rc.1", tags: []string{"v1.2.3-rc.1", "1.2.3-rc.1"}},
		{given: "1.2", err: true},
		{given: "main", err: true},
		{given: "v1.2.3 ", err: true},
	}
	for _, tc := range cases {
		t.Run(tc.given, func(t *testing.T) {
			v, err := ParseVersion(tc.given)
			if tc.err {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.given)
				}
				if agentcli.Exit(err) != agentcli.ExitUsage {
					t.Fatalf("expected exit %d, got %d", agentcli.ExitUsage, agentcli.Exit(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Bare != tc.bare {
				t.Errorf("bare: want %q, got %q", tc.bare, v.Bare)
			}
			if !slices.Equal(v.Tags(), tc.tags) {
				t.Errorf("tags: want %v, got %v", tc.tags, v.Tags())
			}
		})
	}
}
