package rerun

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rerun"
)

const command = rerun.CommandRelease

type runner struct {
	stdout io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// The seams tests replace: the token gates and the endpoints. GitHub is
	// asked for a token only when the tag's push webhook is redelivered.
	requireGitHub   func(ctx context.Context) (authstore.Token, error)
	personGitHub    func(ctx context.Context) (authstore.Token, error)
	requireCircleCI func(ctx context.Context) (authstore.Token, error)
	endpoints       func() agentcli.Endpoints
}

// document is the command's JSON: the rerun's and, when the tag's push
// webhook was redelivered, who did it on GitHub.
type document struct {
	rerun.Document
	// Identity is who read GitHub for the redelivery: "app" or "gh", as in
	// devctl pr wait; absent when GitHub was not read.
	Identity string `json:"identity,omitempty"`
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	doc := newDocument(args)
	err := r.rerun(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := newDocument(nil)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(command, err))
}

func newDocument(args []string) document {
	repository := ""
	if len(args) > 0 {
		repository = args[0]
	}
	return document{Document: rerun.NewDocument(command, repository)}
}

func (r *runner) rerun(ctx context.Context, args []string, doc *document) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: devctl %s <owner/repo> <tag>, got %d argument(s)", command, len(args))
	}
	owner, repo, err := agentcli.ParseRepository(args[0])
	if err != nil {
		return err
	}
	doc.Repository, doc.Tag = owner+"/"+repo, args[1]
	if err := r.gate(false); err != nil {
		return err
	}

	endpoints := r.endpoints()
	retrying := &agentcli.Retrying{Warn: doc.Warn}
	circleci, err := rerun.NewCircleCI(ctx, r.requireCircleCI, endpoints, retrying, doc.Warn)
	if err != nil {
		return err
	}
	tags := []string{args[1]}
	if version, err := releasewait.ParseVersion(args[1]); err == nil {
		tags = version.Tags()
	}
	for _, tag := range tags {
		pipeline, err := circleci.FindPipelineByTag(ctx, owner, repo, tag)
		if err != nil {
			return err
		}
		if pipeline != nil {
			doc.Tag = tag
			// A pipeline without a workflow gets the tag's push again; GitHub
			// is read only then, with the identity devctl pr wait uses.
			redelivery := rerun.Redelivery{
				Ref: "refs/tags/" + tag,
				Hooks: func(ctx context.Context) (rerun.Hooks, string, error) {
					github, token, err := rerun.NewGitHub(ctx, owner, r.requireGitHub, r.personGitHub, endpoints, retrying)
					if err != nil {
						return nil, "", err
					}
					doc.Identity = authexec.Identity(token)
					return github, doc.Identity, nil
				},
			}
			return rerun.FromFailed(ctx, circleci, owner, repo, pipeline, doc.Result, doc.Warn, redelivery)
		}
	}
	return rerun.NoPipeline(fmt.Sprintf("the tag %s of %s/%s among its newest pipelines", strings.Join(tags, " or "), owner, repo))
}
