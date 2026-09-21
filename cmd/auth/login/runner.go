package login

import (
	"context"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const command = "auth login"

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// open is authstore.Open; tests inject an Auth over fakes.
	open func(stderr io.Writer) (*authstore.Auth, error)
}

// document is the command's JSON: the envelope and both identities.
type document struct {
	agentcli.Envelope
	authstore.Status
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	return r.run(context.Background())
}

func (r *runner) run(ctx context.Context) error {
	doc := document{Envelope: agentcli.NewEnvelope(command, time.Now()), Status: authstore.NewStatus()}
	err := r.login(ctx, &doc)
	doc.Finish(time.Now(), agentcli.VerdictGreen, err)
	if err := agentcli.Emit(r.stdout, doc); err != nil {
		return err
	}
	return doc.Err()
}

func (r *runner) login(ctx context.Context, doc *document) error {
	if err := r.flag.Validate(); err != nil {
		return err
	}
	auth, err := r.open(r.stderr)
	if err != nil {
		return err
	}
	progress := agentcli.NewProgress(r.stderr, r.flag.Progress)

	if !r.flag.CircleCIOnly {
		progress.Printf("GitHub: requesting a device code")
		id, err := auth.LoginGitHub(ctx)
		if err != nil {
			return err
		}
		progress.Printf("GitHub: logged in as %s, token stored in the keychain", id.Login)
	}
	if !r.flag.GitHubOnly {
		progress.Printf("CircleCI: starting the authorization")
		id, err := auth.LoginCircleCI(ctx)
		if err != nil {
			return err
		}
		progress.Printf("CircleCI: logged in as %s, token stored in the keychain", id.Login)
	}

	status, err := auth.Status()
	if err != nil {
		return err
	}
	doc.Status = status
	for _, w := range status.CircleCI.Warnings {
		doc.Warn(w)
	}
	return nil
}
