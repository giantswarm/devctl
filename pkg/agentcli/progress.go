package agentcli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Progress writes one line per step to stderr when --progress is set and
// nothing otherwise. Stdout stays the document's.
type Progress struct {
	w io.Writer
}

// NewProgress writes to w; a nil w or enabled false discards.
func NewProgress(w io.Writer, enabled bool) *Progress {
	if !enabled || w == nil {
		return &Progress{}
	}
	return &Progress{w: w}
}

// Printf writes one line.
func (p *Progress) Printf(format string, args ...any) {
	if p == nil || p.w == nil {
		return
	}
	fmt.Fprintf(p.w, format+"\n", args...)
}

// ProgressFlag registers --progress on cmd, bound to v.
func ProgressFlag(cmd *cobra.Command, v *bool) {
	cmd.Flags().BoolVar(v, "progress", false, "Write one line per step to stderr; stdout stays the JSON document")
}
