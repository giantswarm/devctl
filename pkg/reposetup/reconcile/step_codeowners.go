package reconcile

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
// with the desired file and title -- the squash merge commits the title --
// the step reports it and changes nothing. One with the desired file and
// another title, as after an edit that failed, gets its title and description
// rewritten; one with another file -- opened for the team the repository was
// transferred from, an override since added or removed -- is rebuilt too:
// one commit with the desired file on the default branch's head, the branch
// moved to it in one write, so the pull request never shows an empty diff.
// A branch left behind by a closed pull request is moved the same way
// before a new pull request is opened. A default branch that carries the
// desired file costs one read and no pull request listing (the run's
// request budget), so a correction pull request left open beside it is not
// looked at.
func (r *Runner) stepCodeowners(ctx context.Context, s *run, sr *StepResult) error {
	want := s.codeownersSource()
	have, err := r.readCodeowners(ctx, s, s.branch())
	if err != nil {
		return err
	}
	if have.content == want.content {
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
	if len(open) == 0 {
		return s.plan(sr, "open a pull request setting CODEOWNERS to "+want.target, func() error {
			if err := r.stageCodeowners(ctx, s, want, false); err != nil {
				return err
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

	pr := open[0]
	staged, err := r.readCodeowners(ctx, s, codeownersBranch)
	if err != nil {
		return err
	}
	fileStaged := staged.present && staged.content == want.content
	if fileStaged && pr.GetTitle() == want.subject {
		sr.Verdict = VerdictDrift
		sr.Summary = fmt.Sprintf("CODEOWNERS differs; pull request #%d awaits its merge", pr.GetNumber())
		s.reportPending(sr, pr, want)
		return nil
	}
	return s.plan(sr, fmt.Sprintf("update pull request #%d to set CODEOWNERS to %s", pr.GetNumber(), want.target), func() error {
		if !fileStaged {
			if err := r.stageCodeowners(ctx, s, want, true); err != nil {
				return err
			}
		}
		updated, _, err := r.GitHub.PullRequests.Edit(ctx, s.owner, s.name, pr.GetNumber(), &github.PullRequest{
			Title: new(want.subject),
			Body:  new(want.body),
		})
		if err != nil {
			return err
		}
		s.reportPending(sr, updated, want)
		return nil
	})
}

// stageCodeowners commits the desired file on top of the default branch's
// head and points codeownersBranch at that commit in one write, so the
// branch is never equal to its base and a failure before the write leaves
// it as it was. exists says the branch is known to exist (an open pull
// request's head); otherwise it is created, and moved when a closed pull
// request left it behind.
func (r *Runner) stageCodeowners(ctx context.Context, s *run, want codeownersSource, exists bool) error {
	base, _, err := r.GitHub.Git.GetRef(ctx, s.owner, s.name, "heads/"+s.branch())
	if err != nil {
		return err
	}
	head, _, err := r.GitHub.Git.GetCommit(ctx, s.owner, s.name, base.GetObject().GetSHA())
	if err != nil {
		return err
	}
	tree, _, err := r.GitHub.Git.CreateTree(ctx, s.owner, s.name, head.GetTree().GetSHA(), []*github.TreeEntry{{
		Path:    new(codeownersFile),
		Mode:    new("100644"),
		Type:    new("blob"),
		Content: new(want.content),
	}})
	if err != nil {
		return err
	}
	commit, _, err := r.GitHub.Git.CreateCommit(ctx, s.owner, s.name, github.Commit{
		Message: new(want.subject),
		Tree:    &github.Tree{SHA: tree.SHA},
		Parents: []*github.Commit{{SHA: head.SHA}},
	}, nil)
	if err != nil {
		return err
	}

	move := func() error {
		_, _, err := r.GitHub.Git.UpdateRef(ctx, s.owner, s.name, "heads/"+codeownersBranch, github.UpdateRef{SHA: commit.GetSHA(), Force: new(true)})
		return err
	}
	if exists {
		return move()
	}
	_, resp, err := r.GitHub.Git.CreateRef(ctx, s.owner, s.name, github.CreateRef{Ref: "refs/heads/" + codeownersBranch, SHA: commit.GetSHA()})
	if err != nil && resp != nil && resp.StatusCode == http.StatusUnprocessableEntity && strings.Contains(err.Error(), "Reference already exists") {
		// A closed pull request's branch: moved, as an open one's is.
		return move()
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
// file, and its content.
type codeownersFileAt struct {
	present bool
	content string
}

// readCodeowners reads CODEOWNERS at ref. A ref without the file, and a
// path that is not a file, read as absent: empty content.
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
	return codeownersFileAt{present: true, content: content}, nil
}
