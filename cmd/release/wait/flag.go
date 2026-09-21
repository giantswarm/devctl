package wait

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

type flag struct {
	PR       int
	Timeout  time.Duration
	Catalog  bool
	Progress bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().IntVar(&f.PR, "pr", 0, "Wait for the release auto-release tagged from this merged pull request instead of a version")
	cmd.Flags().DurationVar(&f.Timeout, "timeout", releasewait.DefaultTimeout, "Give up after this long (exit 2)")
	cmd.Flags().BoolVar(&f.Catalog, "catalog", false, "Also wait for the catalog index to list every chart")
	agentcli.ProgressFlag(cmd, &f.Progress)
}
