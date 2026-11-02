package promote

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

type flag struct {
	Team     string
	DryRun   bool
	Progress bool
	Version  string
	// Branch promotes on this branch instead of the default one.
	Branch string
	// NoFollow returns once dispatched instead of following the runs.
	NoFollow      bool
	FollowTimeout time.Duration
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Team, "team", "", "Promote every auto-release repository of this team's file in giantswarm/github (e.g. team-bumblebee) instead of named repositories")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Pick and check the candidates, dispatch nothing")
	cmd.Flags().StringVar(&f.Version, "version", "", "Promote this release candidate (vX.Y.Z-rc.N or X.Y.Z-rc.N) of the one named repository instead of the latest: one built while a later one still builds, or the one proven")
	cmd.Flags().StringVar(&f.Branch, "branch", "", "Promote the candidate of this branch of the one named repository instead of the default branch's: a fork line's maintenance branch release-X.Y, which cuts the candidates of its own series")
	cmd.Flags().BoolVar(&f.NoFollow, "no-follow", false, "Return once the workflow is dispatched instead of following its run until the stable gate and the tag are done")
	cmd.Flags().DurationVar(&f.FollowTimeout, "follow-timeout", releasepromote.DefaultFollowTimeout, "How long to follow the dispatched runs; a run still going then leaves its repository dispatched with a warning")
	agentcli.ProgressFlag(cmd, &f.Progress)
}
