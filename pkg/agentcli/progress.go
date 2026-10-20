package agentcli

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// HeartbeatInterval is how often a wait without --progress still says on
// stderr what it is waiting for, so a wait that outlives its caller's
// patience names its cause instead of staying silent.
const HeartbeatInterval = 2 * time.Minute

// Progress writes one line per step to stderr when --progress is set and,
// without it, only the heartbeat of [Progress.Waiting]. Stdout stays the
// document's.
type Progress struct {
	w io.Writer
	// heartbeat receives Waiting's lines when w is nil: stderr without
	// --progress; nil discards them too.
	heartbeat io.Writer
	// last is what Waiting reported last and when.
	last   string
	lastAt time.Time
}

// NewProgress writes every line to w with enabled; without it only the
// heartbeat goes to w. A nil w discards everything.
func NewProgress(w io.Writer, enabled bool) *Progress {
	if w == nil {
		return &Progress{}
	}
	if !enabled {
		return &Progress{heartbeat: w}
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

// Waiting is the heartbeat of a wait without --progress: what the wait still
// waits for at now, written as "waiting for <what>" once per
// [HeartbeatInterval], and at once when it changed, so the stream names every
// cause a wait had and no more than one line every two minutes for the same
// one. With --progress the per-poll lines say it already and Waiting writes
// nothing.
func (p *Progress) Waiting(now time.Time, what string) {
	if p == nil || p.w != nil || p.heartbeat == nil {
		return
	}
	if what == p.last && now.Sub(p.lastAt) < HeartbeatInterval {
		return
	}
	p.last, p.lastAt = what, now
	fmt.Fprintf(p.heartbeat, "waiting for %s\n", what)
}

// ProgressFlag registers --progress on cmd, bound to v.
func ProgressFlag(cmd *cobra.Command, v *bool) {
	cmd.Flags().BoolVar(v, "progress", false, "Write one line per step to stderr; stdout stays the JSON document")
}
