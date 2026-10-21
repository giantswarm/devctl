package prwait

import (
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// Command is the command a wait's document names.
const Command = "pr wait"

// Document is the JSON of a wait: the envelope and the wait's result.
type Document struct {
	agentcli.Envelope
	*Result
	// Identity is who acted on GitHub: "app", a GitHub App's user token (the
	// devctl App login on a laptop, the person's token on the server), or
	// "gh", the person's own gh login for an owner the App is not installed
	// on; empty when the run ended before choosing.
	Identity string `json:"identity"`
}

// NewDocument is the document of a wait started at now.
func NewDocument(now time.Time) Document {
	return Document{
		Envelope: agentcli.NewEnvelope(Command, now),
		Result:   &Result{Checks: []Check{}, Actions: []ActionRun{}},
	}
}

// Take puts a wait's result into the document, its warnings into the
// envelope.
func (d *Document) Take(result *Result) {
	d.Result = result
	for _, w := range result.Warnings {
		d.Warn(w)
	}
}
