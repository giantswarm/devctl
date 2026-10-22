package job

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/ci"
)

const flagLines = "lines"

type flag struct {
	Lines int
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().IntVar(&f.Lines, flagLines, ci.DefaultJobLogLines, "How many lines of the failed steps' output the document keeps")
}

func (f *flag) Validate() error {
	if f.Lines <= 0 {
		return fmt.Errorf("--%s must be positive, got %d", flagLines, f.Lines)
	}
	return nil
}
