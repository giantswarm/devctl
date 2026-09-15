package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// validFlag returns a flag set that Validate accepts, as a base for the cases
// below to spoil one field at a time.
func validFlag() *flag {
	return &flag{
		Name:          "prometheus-operator",
		UpstreamRepo:  "https://github.com/prometheus-community/helm-charts",
		UpstreamChart: "charts/kube-prometheus-stack",
		Team:          "atlas",
		SyncMethod:    methodVendir,
		PatchMethod:   methodScript,
		GithubToken:   "GITHUB_TOKEN",
	}
}

func Test_flag_Validate(t *testing.T) {
	t.Run("accepts a well-formed flag set", func(t *testing.T) {
		require.NoError(t, validFlag().Validate())
	})

	// Name reaches the argument list of a subprocess and Team is joined into a
	// repository path, so neither may read as a flag or climb out of the
	// directory.
	testCases := []struct {
		name  string
		spoil func(*flag)
	}{
		{"empty name", func(f *flag) { f.Name = "" }},
		{"empty team", func(f *flag) { f.Team = "" }},
		{"team escapes the repositories directory", func(f *flag) { f.Team = "../../../../etc/passwd" }},
		{"team carries a path separator", func(f *flag) { f.Team = "atlas/../bumblebee" }},
		{"name reads as a flag", func(f *flag) { f.Name = "--disable-branch-protection" }},
		{"team reads as a flag", func(f *flag) { f.Team = "-rf" }},
		{"name carries a separator", func(f *flag) { f.Name = "app;id" }},
		{"name is uppercase", func(f *flag) { f.Name = "Prometheus" }},
		{"unknown sync method", func(f *flag) { f.SyncMethod = "rsync" }},
		{"unknown patch method", func(f *flag) { f.PatchMethod = "patch" }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := validFlag()
			tc.spoil(f)
			require.Error(t, f.Validate())
		})
	}
}

func Test_execCommand_refusesUnknownBinary(t *testing.T) {
	r := &runner{}

	err := r.execCommand(t.Context(), "", "curl", "https://example.com")
	require.Error(t, err)
	require.True(t, IsExecutionFailed(err))

	for _, command := range []string{"devctl", "git", "vendir"} {
		require.True(t, allowedCommands[command], "%s must stay allowed", command)
	}
}
