package failed

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

type runner struct {
	stdout io.Writer
	gate   func(bool) error
	open   func(context.Context, string, http.RoundTripper, func(string)) (*githubclient.Client, error)
}

type release struct {
	Tag string `json:"tag"`
	URL string `json:"url"`
}

type document struct {
	agentcli.Envelope
	Repository string    `json:"repository"`
	Releases   []release `json:"releases"`
}

func newDocument(args []string) document {
	doc := document{Envelope: agentcli.NewEnvelope(command, time.Now()), Releases: []release{}}
	if len(args) > 0 {
		doc.Repository = args[0]
	}
	return doc
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	doc := newDocument(args)
	err := r.list(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictListed, err)
}

func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := newDocument(nil)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictListed, agentcli.FlagError(command, err))
}

func (r *runner) list(ctx context.Context, args []string, doc *document) error {
	if len(args) != 1 {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "usage: devctl %s <owner/repo>", command)
	}
	owner, repo, err := agentcli.ParseRepository(args[0])
	if err != nil {
		return err
	}
	if err := r.gate(false); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	clock, err := agentcli.SystemClock()
	if err != nil {
		return err
	}
	client, err := r.open(ctx, owner, &agentcli.Retrying{Clock: clock, Warn: doc.Warn}, doc.Warn)
	if err != nil {
		return err
	}
	releases, err := client.ListReleases(ctx, owner, repo)
	if err != nil {
		return err
	}
	for _, rel := range releases {
		if rel.Published && rel.PublishFailed {
			doc.Releases = append(doc.Releases, release{Tag: rel.Tag, URL: rel.URL})
		}
	}
	return nil
}
