package status

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type flag struct {
	Progress bool
}

func (f *flag) Init(cmd *cobra.Command) {
	agentcli.ProgressFlag(cmd, &f.Progress)
}
