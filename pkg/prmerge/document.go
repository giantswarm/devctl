package prmerge

import (
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// Command is the command a merge's document names.
const Command = "pr merge"

// Document is the JSON of a merge: the envelope and the merge's result.
type Document struct {
	agentcli.Envelope
	*Result
	// Identity is who acted on GitHub: "app", a GitHub App's user token (the
	// devctl App login on a laptop, the person's token on the server), or
	// "gh", the person's own gh login for an owner the App is not installed
	// on; empty when the run ended before choosing.
	Identity string `json:"identity"`
}

// NewDocument is the document of a merge started at now that lands with
// method until the merge settles it.
func NewDocument(now time.Time, method string) Document {
	return Document{
		Envelope: agentcli.NewEnvelope(Command, now),
		Result: &Result{
			Result: prwait.Result{Checks: []prwait.Check{}, Actions: []prwait.ActionRun{}},
			Method: method,
		},
	}
}

// Take puts a merge's result into the document, its warnings into the
// envelope.
func (d *Document) Take(result *Result) {
	d.Result = result
	for _, w := range result.Warnings {
		d.Warn(w)
	}
}
