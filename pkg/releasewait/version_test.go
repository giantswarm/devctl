package releasewait

import (
	"errors"
	"slices"
	"strings"
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
		{given: "go/v0.3.0", bare: "0.3.0", tags: []string{"go/v0.3.0"}},
		{given: "go/0.3.0", err: true},
		{given: "Go/v0.3.0", err: true},
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

func TestReleaseTag(t *testing.T) {
	cases := []struct {
		name     string
		tags     []string
		want     string
		conflict []string
	}{
		{name: "none", tags: nil, want: ""},
		{name: "one", tags: []string{"v1.2.3"}, want: "v1.2.3"},
		{name: "a candidate and its stable", tags: []string{"v1.2.3-rc.1", "v1.2.3"}, want: "v1.2.3"},
		{name: "spellings of one version", tags: []string{"1.2.3", "v1.2.3"}, want: "1.2.3"},
		{name: "an upstream pre-release is no release tag", tags: []string{"v1.3.0-alpha.1", "v1.2.3"}, want: "v1.2.3"},
		{name: "a name that is no version", tags: []string{"latest", "v1.2.3"}, want: "v1.2.3"},
		{name: "two versions", tags: []string{"v1.1.4", "v1.2.0"}, conflict: []string{"v1.2.0", "v1.1.4"}},
		{name: "one image release", tags: []string{"go/v0.3.0"}, want: "go/v0.3.0"},
		{name: "a release tag beats an image release", tags: []string{"go/v0.3.0", "v1.2.3"}, want: "v1.2.3"},
		{name: "two image releases", tags: []string{"node/v0.2.1", "go/v0.3.0"}, conflict: []string{"go/v0.3.0", "node/v0.2.1"}},
		{name: "a candidate of another version", tags: []string{"v1.2.0", "v1.3.0-rc.1", "latest"}, conflict: []string{"v1.3.0-rc.1", "v1.2.0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := releaseTag("giantswarm/substrate", "1565be1b0000000000000000000000000000beef", tc.tags)
			if tc.conflict == nil {
				if err != nil || got != tc.want {
					t.Fatalf("want %q, got %q, %v", tc.want, got, err)
				}
				return
			}
			var ambiguous *AmbiguousReleaseError
			if !errors.As(err, &ambiguous) || got != "" {
				t.Fatalf("want the conflict %v, got %q, %v", tc.conflict, got, err)
			}
			if !slices.Equal(ambiguous.Tags, tc.conflict) {
				t.Errorf("tags: want %v, got %v", tc.conflict, ambiguous.Tags)
			}
			if code, verdict := agentcli.Outcome(err); code != agentcli.ExitAmbiguousRelease || verdict != agentcli.VerdictAmbiguousRelease {
				t.Errorf("outcome: %d %s", code, verdict)
			}
			for _, tag := range tc.conflict {
				if !strings.Contains(err.Error(), tag) {
					t.Errorf("reason does not name %s: %s", tag, err)
				}
			}
		})
	}
}
