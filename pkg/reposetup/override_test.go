package reposetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCodeownersOverride(t *testing.T) {
	dir := t.TempDir()
	teamFile := filepath.Join(dir, "repositories", "team-bumblebee.yaml")
	override := filepath.Join(dir, "repositories", "override", "overridden", "CODEOWNERS")
	if err := os.MkdirAll(filepath.Dir(override), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte("* @giantswarm/team-bumblebee @giantswarm/team-other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory with other overrides and no CODEOWNERS.
	if err := os.MkdirAll(filepath.Join(dir, "repositories", "override", "readme-only"), 0o750); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, repository string
		want             []byte
	}{
		{name: "the override verbatim", repository: "overridden", want: []byte("* @giantswarm/team-bumblebee @giantswarm/team-other\n")},
		{name: "no override directory", repository: "plain", want: nil},
		{name: "an override directory without CODEOWNERS", repository: "readme-only", want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadCodeownersOverride(teamFile, tc.repository)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(tc.want) || (got == nil) != (tc.want == nil) {
				t.Errorf("ReadCodeownersOverride(%q) = %q, want %q", tc.repository, got, tc.want)
			}
		})
	}
}

func TestRemoteOverrides(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name      string
		overrides map[string]string
		// repository is read after the listing; want its override,
		// wantGets the requests under repositories/override in total.
		repository string
		want       []byte
		wantGets   int
	}{
		{
			name:       "the override verbatim, one listing and one read",
			overrides:  map[string]string{"overridden": "* @giantswarm/team-other\n", "readme-only": ""},
			repository: "overridden", want: []byte("* @giantswarm/team-other\n"), wantGets: 2,
		},
		{
			name:       "a repository the listing does not name costs no request",
			overrides:  map[string]string{"overridden": "* @giantswarm/team-other\n"},
			repository: "plain", want: nil, wantGets: 1,
		},
		{
			name:       "an override directory without CODEOWNERS",
			overrides:  map[string]string{"readme-only": ""},
			repository: "readme-only", want: nil, wantGets: 2,
		},
		{
			name:       "no override directory at the ref",
			overrides:  nil,
			repository: "plain", want: nil, wantGets: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeTeamFiles{overrides: tc.overrides}
			remote, done := newRemote(t, f)
			defer done()

			overrides, err := remote.Overrides(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got, err := overrides.Codeowners(ctx, tc.repository)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(tc.want) || (got == nil) != (tc.want == nil) {
				t.Errorf("Codeowners(%q) = %q, want %q", tc.repository, got, tc.want)
			}
			if f.overrideGets != tc.wantGets {
				t.Errorf("%d requests under repositories/override, want %d", f.overrideGets, tc.wantGets)
			}
		})
	}
}
