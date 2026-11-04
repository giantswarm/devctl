package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

func Test_AutoReleaseCandidateArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name, missing, pipeline string
		wantError               bool
	}{
		{name: "built candidate", pipeline: "tag-pipeline"},
		{name: "missing chart", pipeline: "tag-pipeline", missing: "chart charts/giantswarm/app:1.3.0-rc.1", wantError: true},
		{name: "missing image", pipeline: "tag-pipeline", missing: "image giantswarm/renamed:1.3.0-rc.1", wantError: true},
		{name: "commit statuses cannot identify the tag", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t, "v1.2.0", "feat: candidate", "v1.3.0-rc.1", "fix: later")
			before := mustGit(t, dir, "rev-parse", "HEAD")
			got, log, err := runStep(t, promoteScript(t), dir, ghStub(t, "true", built),
				"GH_REPO=giantswarm/example", "PROMOTE_VERSION=v1.3.0-rc.1",
				"VERIFIER_FAILURE="+tc.missing, "VERIFIER_PIPELINE="+tc.pipeline)
			args, readErr := os.ReadFile(filepath.Join(dir, "verifier-args")) // #nosec G304 -- fixed filename in t.TempDir
			require.NoError(t, readErr)
			require.Equal(t, "release wait giantswarm/example v1.3.0-rc.1 --timeout 30s\n", string(args))
			if tc.wantError {
				require.Error(t, err, log)
				require.Empty(t, got["rc"])
				require.Contains(t, log, "Candidate not built")
				require.Contains(t, log, tc.missing)
				require.Equal(t, before, mustGit(t, dir, "rev-parse", "HEAD"), "refusal must precede checkout")
			} else {
				require.NoError(t, err, log)
				require.Equal(t, "v1.3.0-rc.1", got["rc"])
			}
		})
	}
}

func Test_AutoReleaseCandidateVerifierWiring(t *testing.T) {
	for _, flavour := range []gen.Flavour{gen.FlavourApp, gen.FlavourCLI, gen.FlavourFork} {
		t.Run(string(flavour), func(t *testing.T) {
			steps, rendered := tagJobStepsFor(t, flavour)
			var names []string
			for _, step := range steps {
				names = append(names, step.Name)
			}
			token := strings.Index(rendered, "name: Create candidate verification token")
			install := strings.Index(rendered, "name: Install candidate verifier")
			resolve := strings.Index(rendered, "name: Resolve the candidate to promote")
			require.GreaterOrEqual(t, token, 0)
			require.Greater(t, install, token)
			require.Greater(t, resolve, install)
			require.Contains(t, names, "Install candidate verifier")
			guard := rendered[token:install]
			require.Contains(t, guard, "if: inputs.release-type == 'stable'")
			require.Contains(t, guard, "repositories: github")
			require.Contains(t, guard, "permission-contents: read")
			require.NotContains(t, guard, ": write")
			for _, step := range steps {
				if step.ID == "promote" {
					require.Equal(t, "${{ secrets.GITHUB_TOKEN }}", step.Env["GITHUB_TOKEN"])
					require.Equal(t, "${{ steps.candidate-token.outputs.token }}", step.Env["DEVCTL_TEAM_GITHUB_TOKEN"])
					require.Equal(t, "${{ secrets.CIRCLECI_API_TOKEN }}", step.Env["CIRCLECI_TOKEN"])
					require.Less(t, strings.Index(step.Run, "devctl release wait"), strings.Index(step.Run, "git checkout"))
				}
			}
		})
	}
}
