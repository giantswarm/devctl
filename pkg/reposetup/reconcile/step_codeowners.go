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
// default branch is protected, so the repair is a pull request from
// codeownersBranch, a branch the step owns. While that pull request is open
// and carries the desired file, title and description the step reports it and
// changes nothing; one that carries anything else -- opened for the team the
// repository was transferred from, an override since added or removed, an
// edit that failed half-way -- is rebuilt: the branch reset to the default
// branch's head, the desired file committed on it, the title and description
// rewritten. A branch left behind by a closed pull request is reset the same
// way before a new pull request is opened. A default branch that carries
// the desired file costs one read and no pull request listing (the run's
// request budget), so a correction pull request left open beside it is not
// looked at.
func (r *Runner) stepCodeowners(ctx context.Context, s *run, sr *StepResult) error {
	want := s.codeownersSource()
	have, err := r.readCodeowners(ctx, s, s.branch())
	if err != nil {
		return err
	}
	if have.present && have.content == want.content {
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
	var pr *github.PullRequest
	if len(open) > 0 {
		pr = open[0]
	}

	if pr != nil {
		staged, err := r.readCodeowners(ctx, s, codeownersBranch)
		if err != nil {
			return err
		}
		if staged.present && staged.content == want.content && pr.GetTitle() == want.subject && pr.GetBody() == want.body {
			sr.Verdict = VerdictDrift
			sr.Summary = fmt.Sprintf("CODEOWNERS differs; pull request #%d awaits its merge", pr.GetNumber())
			s.reportPending(sr, pr, want)
			return nil
		}
	}

	change := "open a pull request setting CODEOWNERS to " + want.target
	if pr != nil {
		change = fmt.Sprintf("update pull request #%d to set CODEOWNERS to %s", pr.GetNumber(), want.target)
	}
	return s.plan(sr, change, func() error {
		if err := r.stageCodeowners(ctx, s, want, have); err != nil {
			return err
		}
		if pr != nil {
			updated, _, err := r.GitHub.PullRequests.Edit(ctx, s.owner, s.name, pr.GetNumber(), &github.PullRequest{
				Title: new(want.subject),
				Body:  new(want.body),
			})
			if err != nil {
				return err
			}
			s.reportPending(sr, updated, want)
			return nil
		}
		created, _, err := r.GitHub.PullRequests.Create(ctx, s.owner, s.name, github.CreatePullRequest{
			Title: new(want.subject),
			Head:  codeownersBranch,
			Base:  s.branch(),
			Body:  new(want.body),
		})
		if err != nil {
			return err
		}
		s.reportPending(sr, created, want)
		return nil
	})
}

// stageCodeowners puts the desired file on codeownersBranch as one commit on
// top of the default branch's head: the branch is created, or reset there
// when it exists (the step owns it), so the pull request never conflicts
// with the default branch. have is CODEOWNERS on the default branch, which
// the reset branch carries too: its blob is the one the commit replaces.
func (r *Runner) stageCodeowners(ctx context.Context, s *run, want codeownersSource, have codeownersFileAt) error {
	base, _, err := r.GitHub.Git.GetRef(ctx, s.owner, s.name, "heads/"+s.branch())
	if err != nil {
		return err
	}
	sha := base.GetObject().GetSHA()
	_, resp, err := r.GitHub.Git.GetRef(ctx, s.owner, s.name, "heads/"+codeownersBranch)
	switch {
	case isNotFound(resp, err):
		if _, _, err := r.GitHub.Git.CreateRef(ctx, s.owner, s.name, github.CreateRef{Ref: "refs/heads/" + codeownersBranch, SHA: sha}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, _, err := r.GitHub.Git.UpdateRef(ctx, s.owner, s.name, "heads/"+codeownersBranch, github.UpdateRef{SHA: sha, Force: new(true)}); err != nil {
			return err
		}
	}

	opts := &github.RepositoryContentFileOptions{
		Message: new(want.subject),
		Content: []byte(want.content),
		Branch:  new(codeownersBranch),
	}
	if have.present {
		opts.SHA = have.sha
		_, _, err = r.GitHub.Repositories.UpdateFile(ctx, s.owner, s.name, codeownersFile, opts)
	} else {
		_, _, err = r.GitHub.Repositories.CreateFile(ctx, s.owner, s.name, codeownersFile, opts)
	}
	return err
}

// reportPending reports the correction pull request as the repair that
// awaits its merge.
func (s *run) reportPending(sr *StepResult, pr *github.PullRequest, want codeownersSource) {
	s.report(sr, FindingPendingPullRequest,
		fmt.Sprintf("pull request #%d sets CODEOWNERS to %s: %s", pr.GetNumber(), want.target, pr.GetHTMLURL()),
		"merge the pull request")
}

// codeownersFileAt is CODEOWNERS at one ref: whether the ref carries the
// file, its content and its blob.
type codeownersFileAt struct {
	present bool
	content string
	sha     *string
}

// readCodeowners reads CODEOWNERS at ref. A ref without the file, and a
// path that is not a file, read as absent.
func (r *Runner) readCodeowners(ctx context.Context, s *run, ref string) (codeownersFileAt, error) {
	fc, _, resp, err := r.GitHub.Repositories.GetContents(ctx, s.owner, s.name, codeownersFile, &github.RepositoryContentGetOptions{Ref: ref})
	switch {
	case isNotFound(resp, err):
		return codeownersFileAt{}, nil
	case err != nil:
		return codeownersFileAt{}, err
	case fc == nil:
		return codeownersFileAt{}, nil
	}
	content, err := fc.GetContent()
	if err != nil {
		return codeownersFileAt{}, err
	}
	return codeownersFileAt{present: true, content: content, sha: fc.SHA}, nil
}
