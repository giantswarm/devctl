package dependabot

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagInterval = "interval"
)

type flag struct {
	Interval  string
	Reviewers []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Interval, flagInterval, "i", "weekly", "Check for daily, weekly or monthly updates (default: weekly).")
	cmd.Flags().StringSliceVarP(&f.Reviewers, "reviewers", "r", []string{}, "Reviewers you want to assign automatically when Dependabot creates a PR, e.g. giantswarm/team-firecracker.")
}

func (f *flag) Validate() error {
	if !gen.IsValidSchedule(f.Interval) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagInterval, strings.Join(gen.AllowedSchedule(), "|"))
	}

	return nil
}
