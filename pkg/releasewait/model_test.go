package releasewait

import (
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

func boolPtr(b bool) *bool { return &b }

// entryWith is a team-file entry with the given gen.ci knobs.
func entryWith(generate *bool, releaseWorkflow string, flavours ...string) *reposetup.Fields {
	ci := &reposetup.CIFields{Generate: generate, ReleaseWorkflow: releaseWorkflow}
	return &reposetup.Fields{Name: "repo", Gen: &reposetup.GenFields{Flavours: flavours, Language: "go", CI: ci}}
}

func TestResolveModels(t *testing.T) {
	autoRelease := []string{"zz_generated.auto_release.yaml", "zz_generated.pre-commit.yaml"}
	legacy := []string{"zz_generated.create_release.yaml", "zz_generated.create_release_pr.yaml"}
	generated := []string{"config.yml", "workflows.yml"}
	handWritten := []string{"config.yml"}

	cases := []struct {
		name      string
		entry     *reposetup.Fields
		workflows []string
		circleci  []string
		want      Models
		wantErr   string
	}{
		{
			name: "generated entry, auto-release files", entry: entryWith(boolPtr(true), ""),
			workflows: autoRelease, circleci: generated,
			want: Models{Release: ReleaseModelAutoRelease, CI: CIModelGenerated},
		},
		{
			name: "generated entry opted back to legacy", entry: entryWith(boolPtr(true), "legacy"),
			workflows: legacy, circleci: generated,
			want: Models{Release: ReleaseModelLegacy, CI: CIModelGenerated},
		},
		{
			name: "entry without gen.ci, legacy files, hand-written CI", entry: &reposetup.Fields{Name: "repo", Gen: &reposetup.GenFields{Flavours: []string{"app"}, Language: "go"}},
			workflows: legacy, circleci: handWritten,
			want: Models{Release: ReleaseModelLegacy, CI: CIModelHandWritten},
		},
		{
			name: "no entry: the files decide", entry: nil,
			workflows: autoRelease, circleci: handWritten,
			want: Models{Release: ReleaseModelAutoRelease, CI: CIModelHandWritten},
		},
		{
			name: "no entry, no CircleCI", entry: nil,
			workflows: autoRelease, circleci: nil,
			want: Models{Release: ReleaseModelAutoRelease, CI: CIModelNone},
		},
		{
			name: "entry without gen says nothing, files say legacy", entry: &reposetup.Fields{Name: "repo"},
			workflows: legacy, circleci: handWritten,
			want: Models{Release: ReleaseModelLegacy, CI: CIModelHandWritten},
		},
		{
			name: "entry says auto-release, files say legacy", entry: entryWith(boolPtr(true), ""),
			workflows: legacy, circleci: generated,
			wantErr: "team-file entry says the release workflow is auto-release but the workflows",
		},
		{
			name: "files carry both release workflows", entry: nil,
			workflows: append(append([]string{}, autoRelease...), legacy...), circleci: handWritten,
			wantErr: "both the auto-release workflow",
		},
		{
			name: "nothing says how the repository releases", entry: &reposetup.Fields{Name: "repo"},
			workflows: []string{"ci.yaml"}, circleci: handWritten,
			wantErr: "cannot tell how the repository releases",
		},
		{
			name: "generate true but no generated config at the tag", entry: entryWith(boolPtr(true), ""),
			workflows: autoRelease, circleci: handWritten,
			wantErr: "gen.ci.generate true but .circleci",
		},
		{
			name: "generate false but the generated config is there", entry: entryWith(boolPtr(false), ""),
			workflows: legacy, circleci: generated,
			wantErr: "gen.ci.generate false but .circleci",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := TagContent{SHA: "0123456789ab", Root: []string{"Dockerfile"}, Workflows: tc.workflows, CircleCI: tc.circleci}
			got, err := ResolveModels(tc.entry, content)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got models %+v", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				if agentcli.Exit(err) != agentcli.ExitUsage {
					t.Fatalf("expected exit %d, got %d", agentcli.ExitUsage, agentcli.Exit(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}
