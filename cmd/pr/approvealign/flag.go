package approvealign

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
	DryRun bool
	Team   string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Only show what would be done without making changes")
	cmd.Flags().StringVar(&f.Team, "team", "", "Sweep every open bot-authored 'Align files' PR in the repositories of this team (repositories/team-<name>.yaml of giantswarm/github), whether or not a review is requested from you; approved, green PRs without auto-merge are merged")
}

func (f *flag) Validate() error {
	if f.Team != "" && strings.ContainsAny(f.Team, "/. ") {
		return microerror.Maskf(invalidFlagError, "--team must be a team name such as planeteers, not a path")
	}
	return nil
}
