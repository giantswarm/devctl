package status

import (
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
	return r.run()
}

func (r *runner) run() error {
	doc := document{Envelope: agentcli.NewEnvelope(command, time.Now()), Status: authstore.NewStatus()}
	err := r.status(&doc)
	doc.Finish(time.Now(), agentcli.VerdictGreen, err)
	if err := agentcli.Emit(r.stdout, doc); err != nil {
		return err
	}
	return doc.Err()
}

func (r *runner) status(doc *document) error {
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
