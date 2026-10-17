package promote

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type flag struct {
	Team     string
	DryRun   bool
	Progress bool
	Version  string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Team, "team", "", "Promote every auto-release repository of this team's file in giantswarm/github (e.g. team-bumblebee) instead of named repositories")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Pick and check the candidates, dispatch nothing")
	cmd.Flags().StringVar(&f.Version, "version", "", "Promote this release candidate (vX.Y.Z-rc.N) of the one named repository instead of the latest: one built while a later one still builds, or the one proven")
	agentcli.ProgressFlag(cmd, &f.Progress)
}
