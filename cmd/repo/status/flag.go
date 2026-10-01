package status

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

type flag struct {
	client.Flags
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd)
}

func (f *flag) Validate() error {
	return f.Flags.Validate()
}
