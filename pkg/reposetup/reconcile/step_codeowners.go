package reconcile

import (
	"context"
	"fmt"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	codeownersFile = "CODEOWNERS"
	// codeownersBranch is the head branch of the correction pull request.
	codeownersBranch = "reposetup/codeowners"
)

// codeownersSource is the CODEOWNERS the codeowners step wants and the
// words that name it: the repository's override verbatim when the request
// carries one, the generated file naming the owning team otherwise — the
// same source align-files writes the file from.
type codeownersSource struct {
	// content is the desired file.
	content string
	// target names it after "sets CODEOWNERS to": @giantswarm/team-x, or
	// the override's path in giantswarm/github.
	target string
	// summary is the step's summary when the file matches.
	summary string
	// subject is the correction's commit subject and pull request title,
	// conventional: whichever of the two the squash merge keeps,
	// auto-release counts the commit.
	subject string
	// body is the correction pull request's description.
	body string
}

// codeownersSource is the desired CODEOWNERS of the run: one source, the
// override when the caller resolved one and the generated file otherwise.
func (s *run) codeownersSource() codeownersSource {
	if s.req.CodeownersOverride != nil {
		override := fmt.Sprintf("the override %s of %s/github", reposetup.CodeownersOverridePath(s.declared), s.owner)
		return codeownersSource{
			content: string(s.req.CodeownersOverride),
			target:  override,
			summary: "matches " + override,
			subject: fmt.Sprintf("chore: set CODEOWNERS to its override in %s/github", s.owner),
			body: fmt.Sprintf("The repository has a CODEOWNERS override, %s of %s/github; CODEOWNERS is the override verbatim, as align-files writes it.\n\nOpened by the repository set-up reconciler.",
				reposetup.CodeownersOverridePath(s.declared), s.owner),
		}
	}
	team := "@" + s.owner + "/" + s.req.Team
	return codeownersSource{
		content: reposetup.Codeowners(s.req.Team),
		target:  team,
		summary: "names " + team,
		subject: "chore: set CODEOWNERS to " + team,
		body: fmt.Sprintf("The repository is declared in repositories/%s.yaml of %s/github; CODEOWNERS follows the declaration.\n\nOpened by the repository set-up reconciler.",
			s.req.Team, s.owner),
	}
}

// stepCodeowners keeps CODEOWNERS what align-files writes: the repository's
// override when it has one, the file naming the owning team otherwise. The
// default branch is protected, so the repair is a pull request; while it is
// open the step reports it and changes nothing.
func (r *Runner) stepCodeowners(ctx context.Context, s *run, sr *StepResult) error {
	want := s.codeownersSource()
	fc, _, resp, err := r.GitHub.Repositories.GetContents(ctx, s.owner, s.name, codeownersFile, &github.RepositoryContentGetOptions{Ref: s.branch()})
	var have string
	switch {
	case isNotFound(resp, err):
	case err != nil:
		return err
	case fc != nil:
		have, err = fc.GetContent()
		if err != nil {
			return err
		}
	}
	if have == want.content {
		sr.Summary = want.summary
		return nil
	}

	open, _, err := r.GitHub.PullRequests.List(ctx, s.owner, s.name, &github.PullRequestListOptions{
		State: "open",
		Head:  s.owner + ":" + codeownersBranch,
	})
	if err != nil {
		return err
	}
	if len(open) > 0 {
		pr := open[0]
		sr.Verdict = VerdictDrift
		sr.Summary = fmt.Sprintf("CODEOWNERS differs; pull request #%d awaits its merge", pr.GetNumber())
		s.report(sr, FindingPendingPullRequest,
			fmt.Sprintf("pull request #%d sets CODEOWNERS to %s: %s", pr.GetNumber(), want.target, pr.GetHTMLURL()),
			"merge the pull request")
		return nil
	}

	return s.plan(sr, "open a pull request setting CODEOWNERS to "+want.target, func() error {
		base, _, err := r.GitHub.Git.GetRef(ctx, s.owner, s.name, "heads/"+s.branch())
		if err != nil {
			return err
		}
		if _, _, err := r.GitHub.Git.CreateRef(ctx, s.owner, s.name, github.CreateRef{
			Ref: "refs/heads/" + codeownersBranch,
			SHA: base.GetObject().GetSHA(),
		}); err != nil {
			return err
		}
		opts := &github.RepositoryContentFileOptions{
			Message: new(want.subject),
			Content: []byte(want.content),
			Branch:  new(codeownersBranch),
		}
		if fc != nil {
			opts.SHA = fc.SHA
			_, _, err = r.GitHub.Repositories.UpdateFile(ctx, s.owner, s.name, codeownersFile, opts)
		} else {
			_, _, err = r.GitHub.Repositories.CreateFile(ctx, s.owner, s.name, codeownersFile, opts)
		}
		if err != nil {
			return err
		}
		pr, _, err := r.GitHub.PullRequests.Create(ctx, s.owner, s.name, github.CreatePullRequest{
			Title: new(want.subject),
			Head:  codeownersBranch,
			Base:  s.branch(),
			Body:  new(want.body),
		})
		if err != nil {
			return err
		}
		s.report(sr, FindingPendingPullRequest,
			fmt.Sprintf("pull request #%d sets CODEOWNERS to %s: %s", pr.GetNumber(), want.target, pr.GetHTMLURL()),
			"merge the pull request")
		return nil
	})
}
