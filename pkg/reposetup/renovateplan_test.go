package reposetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/renovate"
)

func TestPlanRenovate(t *testing.T) {
	on, off := true, false
	reviewers := []string{"team:team-rocket"}
	cases := []struct {
		name      string
		fields    Fields
		hasConfig bool
		want      RenovatePlan
	}{
		{name: "generated CI", fields: Fields{Gen: &GenFields{Flavours: []string{"app"}, CI: &CIFields{Generate: &on}}}, hasConfig: true, want: RenovatePlan{Generate: true}},
		{name: "customer flavour", fields: Fields{Gen: &GenFields{Flavours: []string{"customer"}}}, hasConfig: true, want: RenovatePlan{Generate: true}},
		{name: "no generated CI, no configuration", fields: Fields{Gen: &GenFields{Flavours: []string{"generic"}, CI: &CIFields{Generate: &off}}}, want: RenovatePlan{Generate: true}},
		{name: "no generated CI, a configuration of its own", fields: Fields{ChoreReviewers: reviewers, Gen: &GenFields{Flavours: []string{"generic"}}}, hasConfig: true, want: RenovatePlan{PatchReviewers: true}},
		{name: "a configuration of its own, no reviewers", fields: Fields{Gen: &GenFields{Flavours: []string{"generic"}}}, hasConfig: true},
		{name: "no gen", fields: Fields{ChoreReviewers: reviewers}, want: RenovatePlan{PatchReviewers: true}},
		{name: "fork line", fields: Fields{ChoreReviewers: reviewers, Gen: &GenFields{Flavours: []string{"fork"}, CI: &CIFields{Generate: &on}}}, want: RenovatePlan{PatchReviewers: true}},
		{name: "renovate: false", fields: Fields{Renovate: &off, ChoreReviewers: reviewers, Gen: &GenFields{Flavours: []string{"customer"}, CI: &CIFields{Generate: &on}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, PlanRenovate(tc.fields, tc.hasConfig))
		})
	}
}

// The scaffold and every later align run leave a repository with the same
// Renovate files: the scaffold runs genCommands' Renovate line into an empty
// checkout, align-files then runs what PlanRenovate decides for the result,
// and nothing changes -- for an entry on generated CI, one without it, the
// customer flavour, and an entry that generates nothing.
func TestRenovateScaffoldAndAlignAgree(t *testing.T) {
	on, off := true, false
	reviewers := []string{"team:team-rocket"}
	cases := []struct {
		name   string
		fields Fields
		want   bool // the scaffold carries renovate.json5
	}{
		{name: "generated CI", want: true,
			fields: Fields{Name: "service", ChoreReviewers: reviewers, Gen: &GenFields{Flavours: []string{"app"}, Language: "go", CI: &CIFields{Generate: &on}}}},
		{name: "no generated CI", want: true,
			fields: Fields{Name: "configs", ChoreReviewers: reviewers, Gen: &GenFields{Flavours: []string{"generic"}, Language: "generic", CI: &CIFields{Generate: &off}}}},
		{name: "customer flavour", want: true,
			fields: Fields{Name: "customer", Gen: &GenFields{Flavours: []string{"customer"}, Language: "generic"}}},
		{name: "generates nothing",
			fields: Fields{Name: "upstream-fork", ChoreReviewers: reviewers, Gen: &GenFields{Flavours: []string{"fork"}, Language: "go"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var scaffold [][]string
			for _, c := range genCommands(tc.fields, genContext{}) {
				if c[2] == genRenovate {
					scaffold = append(scaffold, c)
				}
			}
			require.NoError(t, runGen(context.Background(), dir, nil, scaffold))
			created := renovateFiles(t, dir)
			require.Equal(t, tc.want, len(created) > 0, created)

			hasConfig, err := renovate.HasConfig(dir)
			require.NoError(t, err)
			plan := PlanRenovate(tc.fields, hasConfig)
			var align [][]string
			switch {
			case plan.Generate:
				align = append(align, []string{"devctl", "gen", genRenovate})
				align[0] = append(align[0], renovateArgs(tc.fields, tc.fields.Gen.CI != nil && *tc.fields.Gen.CI.Generate, func(string, string) bool { return true })...)
			case plan.PatchReviewers && hasConfig:
				align = append(align, append([]string{"devctl", "gen", genRenovate, "reviewers"}, reviewerArgs(tc.fields)...))
			}
			require.NoError(t, runGen(context.Background(), dir, nil, align))
			require.Equal(t, created, renovateFiles(t, dir), "the first align run changes nothing")
		})
	}
}

// renovateFiles maps every Renovate configuration in dir to its content.
func renovateFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, p := range renovate.ConfigPaths {
		content, err := os.ReadFile(filepath.Clean(filepath.Join(dir, p)))
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		files[p] = string(content)
	}
	return files
}
