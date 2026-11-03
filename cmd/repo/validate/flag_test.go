package validate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

func TestModeDefaults(t *testing.T) {
	f := &flag{TeamFile: "repositories/team-bumblebee.yaml"}
	require.NoError(t, f.Validate())
	require.Equal(t, reposetup.ModeExisting, f.mode(), "a whole file is on main already")

	f.Entries = []string{"my-service"}
	require.Equal(t, reposetup.ModeCreate, f.mode(), "the entries named are being added")

	f.Mode = "existing"
	require.NoError(t, f.Validate())
	require.Equal(t, reposetup.ModeExisting, f.mode(), "--mode wins")

	f.Mode = "repair"
	err := f.Validate()
	require.True(t, IsInvalidFlag(err), "%v", err)
	require.Contains(t, err.Error(), "want create or existing")
}

// The manager answers for declarations being added; --local and CI run
// devctl's own engine and say why; what only the local engine does is
// refused without --local, never run locally in the manager's place.
func TestLocalReason(t *testing.T) {
	added := []string{"my-service"}
	cases := []struct {
		name string
		flag flag
		ci   string

		want    string // the reason; empty means the manager
		refused string // substring of the refusal; empty means none
	}{
		{name: "the entries being added: the manager", flag: flag{Entries: added, Owner: reposetup.DefaultOwner}},
		{name: "--owner left at its default", flag: flag{Entries: added}},
		{name: "--local", flag: flag{Entries: added, Local: true}, want: "--local"},
		{name: "--local in existing mode", flag: flag{Mode: "existing", Local: true}, want: "--local"},
		{name: "CI: the keychain is not read", flag: flag{Entries: added}, ci: "true", want: "$CI is set, so the keychain is not read"},
		{name: "CI in existing mode, as the validation check runs it", flag: flag{Mode: "existing", Owner: reposetup.DefaultOwner}, ci: "true", want: "$CI is set, so the keychain is not read"},
		{name: "existing mode takes --local", flag: flag{}, refused: "existing mode is the local engine's: pass --local"},
		{name: "--mode existing takes --local", flag: flag{Entries: added, Mode: "existing"}, refused: "existing mode is the local engine's"},
		{name: "--schema takes --local", flag: flag{Entries: added, Schema: "schema.json"}, refused: "--schema is the local engine's"},
		{name: "--author takes --local", flag: flag{Entries: added, Author: "octocat"}, refused: "--author and --author-team are the local engine's"},
		{name: "--author-team takes --local", flag: flag{Entries: added, AuthorTeams: []string{"team-rocket"}}, refused: "--author and --author-team are the local engine's"},
		{name: "another --owner takes --local", flag: flag{Entries: added, Owner: "acme"}, refused: "--owner acme is the local engine's"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(authstore.EnvCI, tc.ci)
			reason, err := tc.flag.localReason()
			if tc.refused != "" {
				require.True(t, IsInvalidFlag(err), "%v", err)
				require.Contains(t, err.Error(), tc.refused)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, reason)
		})
	}
}
