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

// stepCodeowners keeps CODEOWNERS naming the owning team. The default
// branch is protected, so the repair is a pull request; while it is open
// the step reports it and changes nothing.
func (r *Runner) stepCodeowners(ctx context.Context, s *run, sr *StepResult) error {
	want := reposetup.Codeowners(s.req.Team)
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
	if have == want {
		sr.Summary = "names @" + s.owner + "/" + s.req.Team
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
			fmt.Sprintf("pull request #%d sets CODEOWNERS to @%s/%s: %s", pr.GetNumber(), s.owner, s.req.Team, pr.GetHTMLURL()),
			"merge the pull request")
		return nil
	}

	return s.plan(sr, fmt.Sprintf("open a pull request setting CODEOWNERS to @%s/%s", s.owner, s.req.Team), func() error {
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
		// Conventional like the pull request's title: whichever of the two
		// the squash merge keeps, auto-release counts the commit.
		subject := fmt.Sprintf("chore: set CODEOWNERS to @%s/%s", s.owner, s.req.Team)
		opts := &github.RepositoryContentFileOptions{
			Message: new(subject),
			Content: []byte(want),
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
			Title: new(subject),
			Head:  codeownersBranch,
			Base:  s.branch(),
			Body: new(fmt.Sprintf("The repository is declared in repositories/%s.yaml of %s/github; CODEOWNERS follows the declaration.\n\nOpened by the repository set-up reconciler.",
				s.req.Team, s.owner)),
		})
		if err != nil {
			return err
		}
		s.report(sr, FindingPendingPullRequest,
			fmt.Sprintf("pull request #%d sets CODEOWNERS to @%s/%s: %s", pr.GetNumber(), s.owner, s.req.Team, pr.GetHTMLURL()),
			"merge the pull request")
		return nil
	})
}
