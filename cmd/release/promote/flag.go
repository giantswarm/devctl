package promote

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type flag struct {
	Team     string
	DryRun   bool
	Progress bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Team, "team", "", "Promote every auto-release repository of this team's file in giantswarm/github (e.g. team-bumblebee) instead of named repositories")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Pick and check the candidates, dispatch nothing")
	agentcli.ProgressFlag(cmd, &f.Progress)
}
