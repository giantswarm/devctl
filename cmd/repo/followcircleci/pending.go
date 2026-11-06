package followcircleci

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// Outcome is what happened to one pending repository.
type Outcome string

const (
	// OutcomeCreated: the pass created the GitHub App pipeline or its trigger.
	OutcomeCreated Outcome = "created"
	// OutcomeAlreadySetUp: the pipeline and its trigger were in place.
	OutcomeAlreadySetUp Outcome = "already-set-up"
	// OutcomeRefused: CircleCI refused the person the write.
	OutcomeRefused Outcome = "refused"
	// OutcomeSkipped: the pass stopped before the repository.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeFailed: the set-up failed for another reason; the next pass
	// tries again.
	OutcomeFailed Outcome = "failed"
)

// listTimeout bounds the inventory read.
const listTimeout = 60 * time.Second

// pendingLimit is how many pending repositories one pass reads.
const pendingLimit = 100

// RunRef is one pipeline run the set-up started.
type RunRef struct {
	Ref      string `json:"ref"`
	Pipeline int64  `json:"pipeline"`
}

// Entry is one pending repository and what the pass did to it.
type Entry struct {
	Repository string   `json:"repository"`
	Outcome    Outcome  `json:"outcome"`
	Reason     string   `json:"reason,omitempty"`
	Runs       []RunRef `json:"runs,omitempty"`
}

// Summary is a pass over the pending repositories.
type Summary struct {
	Pending      int     `json:"pending"`
	Repositories []Entry `json:"repositories"`
}

// pendingPass sets up the GitHub App pipeline of every repository of the
// person's teams that the inventory reports circleci-pipeline-missing for.
type pendingPass struct {
	open   client.Opener
	openCI func(ctx context.Context, owner string, warn func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error)
	warn   func(string)
}

func (p *pendingPass) run(ctx context.Context) (*Summary, error) {
	repos, err := p.pending(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	summary := &Summary{Pending: len(repos), Repositories: []Entry{}}
	type clients struct {
		gh circleciapp.GitHub
		ci circleciapp.CircleCI
	}
	opened := map[string]clients{}
	var failed, refused []string
	wrote := false
	for i, slug := range repos {
		owner, repo, _ := strings.Cut(slug, "/")
		c, ok := opened[owner]
		if !ok {
			gh, ci, err := p.openCI(ctx, owner, p.warnf)
			if err != nil {
				skip(summary, repos[i:], loginReason(err))
				return summary, microerror.Mask(err)
			}
			c = clients{gh: gh, ci: ci}
			opened[owner] = c
		}
		report, err := circleciapp.Setup(ctx, c.ci, c.gh, owner, repo, circleciapp.Options{})
		switch {
		case circleciclient.IsForbidden(err):
			// A Read-only login, or a member CircleCI does not let write on
			// this repository: the others may still work.
			summary.Repositories = append(summary.Repositories, Entry{Repository: slug, Outcome: OutcomeRefused, Reason: "CircleCI refused the write (403)"})
			refused = append(refused, slug)
			continue
		case err != nil && ctx.Err() != nil:
			skip(summary, repos[i:], "the pass ran out of time")
			return summary, microerror.Mask(ctx.Err())
		case err != nil:
			summary.Repositories = append(summary.Repositories, Entry{Repository: slug, Outcome: OutcomeFailed, Reason: err.Error()})
			failed = append(failed, slug)
			continue
		}
		e := entry(slug, report)
		if e.Outcome == OutcomeCreated {
			wrote = true
		}
		summary.Repositories = append(summary.Repositories, e)
	}
	if len(refused) > 0 {
		admin := fmt.Sprintf("a repository admin has to run %s", followCommands(refused))
		if !wrote {
			// Nothing written: most likely a login granted Read only.
			cause := fmt.Sprintf("CircleCI refused to create the pipeline of %s; when your login granted Write already, %s", strings.Join(refused, ", "), admin)
			return summary, microerror.Mask(authstore.CircleCIWriteRequired(cause))
		}
		return summary, agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused, "CircleCI refused you the pipeline of %s, while your login writes elsewhere: %s", strings.Join(refused, ", "), admin)
	}
	if len(failed) > 0 {
		// A failed set-up is CircleCI's or GitHub's answer: the next pass
		// may succeed.
		return summary, &agentcli.RemoteError{Err: microerror.Maskf(pendingFailedError, "the set-up of %s failed; the next pass tries again", strings.Join(failed, ", "))}
	}
	return summary, nil
}

// followCommands names the follow-circleci command of each repository.
func followCommands(repos []string) string {
	var cmds []string
	for _, slug := range repos {
		cmds = append(cmds, fmt.Sprintf("`devctl repo follow-circleci %s`", slug))
	}
	return strings.Join(cmds, ", ")
}

// loginReason is why the pass skipped the repositories when the clients
// would not open: the login that is missing, or the error.
func loginReason(err error) string {
	var authErr *authstore.AuthRequiredError
	if errors.As(err, &authErr) {
		return fmt.Sprintf("no usable %s login", authErr.Identity)
	}
	return err.Error()
}

// skip records repos as skipped for reason.
func skip(summary *Summary, repos []string, reason string) {
	for _, slug := range repos {
		summary.Repositories = append(summary.Repositories, Entry{Repository: slug, Outcome: OutcomeSkipped, Reason: reason})
	}
}

// pending lists the repositories of the person's teams waiting for a
// CircleCI pipeline: declared, in production, not archived, still on GitHub.
// The manager's own lifecycle filter is not asked: its "active" matches no
// lifecycle declared, not the schema's default "production".
func (p *pendingPass) pending(ctx context.Context) ([]string, error) {
	session, err := p.open(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolListRepositories, map[string]any{
		"scope":    "mine",
		"finding":  string(reconcile.FindingCircleCIPipelineMissing),
		"archived": false,
		"limit":    pendingLimit,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	listing, err := client.Decode[manager.Listing](manager.ToolListRepositories, payload)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var repos []string
	for _, row := range listing.Repositories {
		if row.Team == "" || !live(row.Lifecycle) || row.Archived || row.Gone {
			continue
		}
		if _, _, ok := strings.Cut(row.Repository, "/"); !ok {
			continue
		}
		repos = append(repos, row.Repository)
	}
	return repos, nil
}

// live says whether a declared lifecycle builds: none declared (the
// schema's default) or production.
func live(lifecycle string) bool {
	return lifecycle == "" || lifecycle == "production"
}

func (p *pendingPass) warnf(w string) {
	if w != "" && p.warn != nil {
		p.warn(w)
	}
}

func entry(slug string, report *circleciapp.Report) Entry {
	e := Entry{Repository: slug, Outcome: OutcomeAlreadySetUp}
	if report.Result != nil && (report.Result.CreatedDefinition || report.Result.CreatedTrigger) {
		e.Outcome = OutcomeCreated
	}
	for _, run := range report.Runs {
		e.Runs = append(e.Runs, RunRef{Ref: run.Ref.String(), Pipeline: run.Pipeline.Number})
	}
	return e
}
