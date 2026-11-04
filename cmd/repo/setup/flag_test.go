package setup

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func setupFlags(t *testing.T, args ...string) *flag {
	t.Helper()
	f := &flag{}
	cmd := &cobra.Command{}
	f.Init(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	return f
}

func TestValidateChecksFilter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "defaults"},
		{name: "empty filter", args: []string{"--checks-filter="}},
		{name: "valid filter", args: []string{"--checks-filter=^optional-"}},
		{name: "invalid filter", args: []string{"--checks-filter=["}, wantErr: true},
		{name: "explicit checks override the filter", args: []string{"--checks=run-tests", "--checks-filter=["}},
		{name: "empty checks override the filter", args: []string{"--checks=", "--checks-filter=["}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := setupFlags(t, tc.args...).Validate()
			if tc.wantErr {
				require.True(t, IsInvalidFlag(err), "%v", err)
				require.ErrorContains(t, err, "--checks-filter")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunRejectsInvalidChecksFilterBeforeClient(t *testing.T) {
	r := &runner{
		flag:   setupFlags(t, "--checks-filter=["),
		logger: logrus.New(),
		stdout: io.Discard,
		stderr: io.Discard,
	}
	// Invalid input must be rejected before token lookup or repository writes.
	// The invalid slug also prevents a client being built if validation regresses.
	err := r.Run(&cobra.Command{}, []string{"invalid-slug"})
	require.True(t, IsInvalidFlag(err), "%v", err)
}
