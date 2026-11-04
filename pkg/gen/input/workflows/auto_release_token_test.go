package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// A GITHUB_TOKEN-created tag cannot start push/release workflows. Check the
// rendered authentication boundary, including no-tag runs and both release
// policies, without creating a real token, tag or release.
func Test_AutoReleaseReleaseToken(t *testing.T) {
	testCases := []struct {
		name   string
		config Config
	}{
		{name: "case 0: app", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}}},
		{name: "case 1: cli", config: Config{Flavours: gen.FlavourSlice{gen.FlavourCLI}}},
		{name: "case 2: generic", config: Config{Flavours: gen.FlavourSlice{gen.FlavourGeneric}}},
		{name: "case 3: fork", config: Config{Flavours: gen.FlavourSlice{gen.FlavourFork}, ReleaseBranch: "giantswarm"}},
		{name: "case 4: patch candidates", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, PatchCandidates: true}},
		{name: "case 5: fork maintenance", config: Config{
			Flavours: gen.FlavourSlice{gen.FlavourFork}, ReleaseBranch: "giantswarm", MaintenanceBranches: true, PatchCandidates: true,
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := New(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			rendered := renderInput(t, w.AutoRelease())
			var wf struct {
				Permissions map[string]string `json:"permissions"`
				Jobs        map[string]struct {
					Permissions map[string]string `json:"permissions"`
					Steps       []struct {
						ID              string            `json:"id"`
						Uses            string            `json:"uses"`
						If              string            `json:"if"`
						ContinueOnError bool              `json:"continue-on-error"`
						Env             map[string]string `json:"env"`
						With            map[string]string `json:"with"`
					} `json:"steps"`
				} `json:"jobs"`
			}
			if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
				t.Fatalf("parse rendered workflow: %v", err)
			}
			if len(wf.Permissions) != 0 {
				t.Errorf("workflow permissions = %v, want none", wf.Permissions)
			}
			job := wf.Jobs["tag"]
			if job.Permissions["contents"] != "read" {
				t.Errorf("GITHUB_TOKEN contents permission = %q, want read", job.Permissions["contents"])
			}

			ids := make([]string, 0, len(job.Steps))
			for _, s := range job.Steps {
				ids = append(ids, s.ID)
			}
			tokenAt, releaseAt := slices.Index(ids, "release-token"), slices.Index(ids, "release")
			if tokenAt < 0 || releaseAt < 0 {
				t.Fatalf("missing token or release step: %v", ids)
			}
			for _, guard := range []string{"decide", "gate", "one-version"} {
				if at := slices.Index(ids, guard); at < 0 || at >= tokenAt {
					t.Errorf("%s must precede token creation: %v", guard, ids)
				}
			}
			if tokenAt >= releaseAt {
				t.Fatalf("token must precede release: %v", ids)
			}
			token, release := job.Steps[tokenAt], job.Steps[releaseAt]
			if !strings.HasPrefix(token.Uses, "actions/create-github-app-token@") {
				t.Errorf("token action = %q, want create-github-app-token", token.Uses)
			}
			wantInputs := map[string]string{
				"client-id":           "${{ secrets.RELEASE_BOT_CLIENT_ID }}",
				"private-key":         "${{ secrets.RELEASE_BOT_PRIVATE_KEY }}",
				"permission-contents": "write",
			}
			if diff := cmp.Diff(wantInputs, token.With); diff != "" {
				t.Errorf("App inputs (-want +got):\n%s", diff)
			}
			if token.ContinueOnError || release.ContinueOnError {
				t.Error("token/release failures must stop the job")
			}
			for _, s := range []int{tokenAt, releaseAt} {
				if job.Steps[s].If != "steps.decide.outputs.tag != ''" {
					t.Errorf("%s condition = %q, want selected tags only", ids[s], job.Steps[s].If)
				}
			}
			if release.Env["GH_TOKEN"] != "${{ steps.release-token.outputs.token }}" {
				t.Errorf("release GH_TOKEN = %q, want the App token without a GITHUB_TOKEN fallback", release.Env["GH_TOKEN"])
			}
		})
	}
}

// Run the shipped release shell with a fail-closed gh stub. It checks the
// token selected by the workflow and the complete atomic-release request for
// candidates, patches, promotions and maintenance releases.
func Test_AutoReleaseReleaseRequest(t *testing.T) {
	release := tagJobStep(t, "release")
	for _, tc := range []struct {
		name, branch, tag, prerelease string
		flags                         []string
	}{
		{name: "case 0: candidate", branch: "main", tag: "v1.3.0-rc.1", prerelease: "true", flags: []string{"--prerelease"}},
		{name: "case 1: patch", branch: "main", tag: "v1.2.4", prerelease: "false"},
		{name: "case 2: promotion", branch: "main", tag: "v1.3.0", prerelease: "false"},
		{name: "case 3: maintenance", branch: "release-v1.x", tag: "v1.2.5", prerelease: "false", flags: []string{"--latest=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, dir := t.TempDir(), t.TempDir()
			stub := `#!/bin/bash
set -euo pipefail
[[ "$GH_TOKEN" == fixture-app-token && "$GH_REPO" == fixture/repository ]]
[[ "$(<release-notes.md)" == 'fixture release notes' ]]
printf '%s\n' "$@" > gh-args
`
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o700); err != nil { // #nosec G306 -- test-only executable stub
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "release-notes.md"), []byte("fixture release notes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			values := map[string]string{ // #nosec G101 -- synthetic token checked only by the isolated gh stub
				"${{ steps.release-token.outputs.token }}": "fixture-app-token",
				"${{ github.repository }}":                 "fixture/repository",
				"${{ steps.decide.outputs.tag }}":          tc.tag,
				"${{ steps.decide.outputs.prerelease }}":   tc.prerelease,
				"${{ steps.decide.outputs.target }}":       strings.Repeat("a", 40),
			}
			env := []string{"PATH=" + bin, "GITHUB_REF_NAME=" + tc.branch}
			for k, expression := range release.Env {
				value, ok := values[expression]
				if !ok {
					t.Fatalf("unexpected release environment expression for %s: %q", k, expression)
				}
				env = append(env, k+"="+value)
			}
			if _, log, err := runStep(t, release.Run, dir, env...); err != nil {
				t.Fatalf("release failed: %v\n%s", err, log)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "gh-args")) // #nosec G304 -- path built from t.TempDir
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"release", "create", tc.tag, "--title", tc.tag, "--notes-file", "release-notes.md", "--target", strings.Repeat("a", 40)}
			want = append(want, tc.flags...)
			if diff := cmp.Diff(want, strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")); diff != "" {
				t.Errorf("gh arguments (-want +got):\n%s", diff)
			}
		})
	}
}
