package rerun

import (
	"time"

	"github.com/spf13/cobra"
)

const flagWedgedAfter = "wedged-after"

// defaultWedgedAfter is far beyond any tag build: a workflow running that
// long has jobs hung on CircleCI's side.
const defaultWedgedAfter = 2 * time.Hour

type flag struct {
	WedgedAfter time.Duration
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&f.WedgedAfter, flagWedgedAfter, defaultWedgedAfter, "Cancel and rerun a workflow that has been running longer than this, its jobs hung on CircleCI's side; 0 leaves running workflows alone")
}
