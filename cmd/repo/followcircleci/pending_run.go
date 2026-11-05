package followcircleci

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// command is the command's name in its JSON document.
const command = "repo follow-circleci"

// pendingDocument is --pending's JSON document.
type pendingDocument struct {
	agentcli.Envelope
	Summary
}

// runPending sets up every pending repository of the person's teams.
func (r *runner) runPending(ctx context.Context) error {
	started := time.Now()
	p := &pendingPass{open: r.openSession, openCI: r.open, warn: func(w string) { fmt.Fprintln(r.stderr, w) }}
	summary, err := p.run(ctx)
	if r.flag.JSON() {
		doc := pendingDocument{Envelope: agentcli.NewEnvelope(command, started), Summary: Summary{Repositories: []Entry{}}}
		if summary != nil {
			doc.Summary = *summary
		}
		return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
	}
	// Without the list there is nothing to print but the error.
	if summary != nil {
		printSummary(r.stdout, summary)
	}
	return microerror.Mask(err)
}

// printSummary writes one line per repository.
func printSummary(w io.Writer, s *Summary) {
	if s.Pending == 0 {
		fmt.Fprintln(w, "no repository of your teams waits for a CircleCI pipeline")
		return
	}
	for _, e := range s.Repositories {
		fmt.Fprintf(w, "%s: %s\n", e.Repository, describe(e))
	}
}

// describe says in words what the pass did to one repository.
func describe(e Entry) string {
	var what string
	switch e.Outcome {
	case OutcomeCreated:
		what = "created the GitHub App pipeline"
	case OutcomeAlreadySetUp:
		what = "set up already"
	default:
		what = string(e.Outcome)
		if e.Reason != "" {
			what += ": " + e.Reason
		}
	}
	if len(e.Runs) > 0 {
		var runs []string
		for _, run := range e.Runs {
			runs = append(runs, fmt.Sprintf("%s (pipeline %d)", run.Ref, run.Pipeline))
		}
		what += ", ran " + strings.Join(runs, ", ")
	}
	return what
}
