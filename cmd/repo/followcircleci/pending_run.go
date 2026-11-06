package followcircleci

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// command is the command's name in its JSON document.
const command = "repo follow-circleci"

// pendingDocument is --pending's JSON document.
type pendingDocument struct {
	agentcli.Envelope
	Summary
}

// newDocument starts --pending's document, with an empty list.
func newDocument() pendingDocument {
	return pendingDocument{Envelope: agentcli.NewEnvelope(command, time.Now()), Summary: Summary{Repositories: []Entry{}}}
}

// FlagError reports a flag cobra could not parse as the document under
// --output json, as the agent-facing commands do.
func (r *runner) FlagError(c *cobra.Command, err error) error {
	// pflag stops at the first bad flag: an -o json after it is not parsed.
	if !r.flag.JSON() && !jsonRequested(os.Args[1:]) {
		// devctl's own handler: the usage line and the pointer to --help.
		if c.HasParent() {
			return c.Parent().FlagErrorFunc()(c, err)
		}
		return err
	}
	doc := newDocument()
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(command, err))
}

// jsonRequested says whether args ask for --output json.
func jsonRequested(args []string) bool {
	for i, a := range args {
		switch {
		case a == "--":
			return false
		case a == "-ojson" || a == "--output=json" || a == "-o=json":
			return true
		case (a == "-o" || a == "--output") && i+1 < len(args) && args[i+1] == "json":
			return true
		}
	}
	return false
}

// runPending sets up every pending repository of the person's teams.
func (r *runner) runPending(ctx context.Context) error {
	doc := newDocument()
	warn := func(w string) { fmt.Fprintln(r.stderr, w) }
	if r.flag.JSON() {
		// The document is stdout's only output: warnings go in it.
		warn = doc.Warn
	}
	p := &pendingPass{open: r.openSession, openCI: r.open, warn: warn}
	summary, err := p.run(ctx)
	if r.flag.JSON() {
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
		if e.Reason != "" {
			what = e.Reason
		}
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
