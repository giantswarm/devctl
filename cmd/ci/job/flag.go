package job

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/ci"
)

const flagLines = "lines"

type flag struct {
	Lines      int
	StepOutput bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().IntVar(&f.Lines, flagLines, ci.DefaultJobLogLines, "Lines to keep in the failed output tail and in each --step-output excerpt")
	cmd.Flags().BoolVar(&f.StepOutput, "step-output", false, "Include the first and last lines of every step's output, including successful steps")
}

func (f *flag) Validate() error {
	if f.Lines <= 0 {
		return fmt.Errorf("--%s must be positive, got %d", flagLines, f.Lines)
	}
	return nil
}
