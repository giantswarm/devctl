package create

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

func TestCommandRejectsOtherCreationOwner(t *testing.T) {
	// CI and an explicitly empty synthetic variable prohibit keychain access
	// and prevent any inherited credential from being used. Flag validation
	// must reject the destination mismatch even before requesting a token.
	t.Setenv("CI", "true")
	t.Setenv("DEVCTL_CREATE_OWNER_TEST_TOKEN", "")
	for _, owner := range []string{"fixture-org", ""} {
		t.Run("owner="+owner, func(t *testing.T) {
			cmd, err := New(Config{Logger: logrus.New(), Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{
				"--team", "fixture", "--name", "fixture-repo", "--owner", owner,
				"--component-type", "service", "--flavour", "generic", "--language", "generic",
				"--github-token-envvar", "DEVCTL_CREATE_OWNER_TEST_TOKEN",
			})
			err = cmd.Execute()
			require.True(t, IsInvalidFlag(err), "reject the wrong creation owner before authentication: %v", err)
			require.NotErrorIs(t, err, authstore.ErrAuthRequired)
			require.Contains(t, err.Error(), "--owner")
			require.Contains(t, err.Error(), "giantswarm")
		})
	}
}

func TestCreationOwnerValidation(t *testing.T) {
	for _, tc := range []struct {
		name, owner   string
		dryRun, local bool
	}{
		{name: "normal creation", owner: reposetup.DefaultOwner},
		{name: "normal preview", owner: reposetup.DefaultOwner, dryRun: true},
		{name: "another organisation local preview", owner: "fixture-org", dryRun: true, local: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &flag{Team: "fixture", Name: "fixture-repo", Owner: tc.owner, DryRun: tc.dryRun, Local: tc.local, Output: outputJSON}
			require.NoError(t, f.Validate())
			require.Equal(t, tc.owner, f.Owner)
			require.Equal(t, "team-fixture", f.Team)
		})
	}
}
