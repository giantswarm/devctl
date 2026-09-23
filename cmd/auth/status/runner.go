package status

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const command = "auth status"

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// open is authstore.Open; tests inject an Auth over a file store.
	open func(stderr io.Writer) (*authstore.Auth, error)
}

// document is the command's JSON: the envelope and both identities.
type document struct {
	agentcli.Envelope
	authstore.Status
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	return r.run(args)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := newDocument()
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(command, err))
}

func (r *runner) run(args []string) error {
	doc := newDocument()
	err := r.status(args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
}

func newDocument() document {
	return document{Envelope: agentcli.NewEnvelope(command, time.Now()), Status: authstore.NewStatus()}
}

func (r *runner) status(args []string, doc *document) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: devctl %s [flags], got %d argument(s)", command, len(args))
	}
	auth, err := r.open(r.stderr)
	if err != nil {
		return err
	}
	progress := agentcli.NewProgress(r.stderr, r.flag.Progress)
	progress.Printf("reading the keychain")
	status, err := auth.Status()
	if err != nil {
		return err
	}
	doc.Status = status
	for _, w := range status.CircleCI.Warnings {
		doc.Warn(w)
	}
	return status.Check()
}
