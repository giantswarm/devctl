package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// orgRoleAdmin is the role GitHub reports for an organization owner.
const orgRoleAdmin = "admin"

// CreateRequest is one creation of one declared repository as the caller:
// the create and scaffold steps of [Runner.Run], standalone, with whatever
// GitHub client the [Runner] holds — the person's, so the repository is
// created as them and they are its admin.
type CreateRequest struct {
	// Owner is the GitHub organization; empty means reposetup.DefaultOwner.
	Owner string
	// Team is the slug of the team file the entry is declared in.
	Team string
	// Entry is an accepted entry of the dry run; its Rendered declaration
	// gives the name, the description and the visibility.
	Entry reposetup.Entry
	// Mode is check or repair; empty means check. [ModeCheck] is the dry
	// run: the caller's organization role is read and the two steps plan
	// what they would do, nothing is written.
	Mode Mode
	// RenderOptions selects among the template's options for the scaffold.
	RenderOptions map[string]string
}

// CreateResult is the outcome of [Runner.Create].
type CreateResult struct {
	// Repository is owner/name.
	Repository string `json:"repository"`
	// URL is the repository on GitHub; empty in a dry run of a repository
	// that does not exist yet.
	URL string `json:"url,omitempty"`
	// Created says this run created the repository; false when it existed
	// (a resumed creation) or in a dry run.
	Created bool `json:"created"`
	// ScaffoldCommit is the commit holding the scaffold at the head of the
	// default branch — the one this run pushed, or the one it found.
	ScaffoldCommit string    `json:"scaffoldCommit,omitempty"`
	Mode           Mode      `json:"mode"`
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt"`
	// Steps are the create and the scaffold step, in that order.
	Steps []StepResult `json:"steps"`
}

// Step returns the result of step, or nil when the run did not execute it.
func (r *CreateResult) Step(step Step) *StepResult {
	for i := range r.Steps {
		if r.Steps[i].Step == step {
			return &r.Steps[i]
		}
	}
	return nil
}

// Failed returns the steps that could not run to their end.
func (r *CreateResult) Failed() []StepResult {
	var failed []StepResult
	for _, s := range r.Steps {
		if s.Verdict == VerdictFailed {
			failed = append(failed, s)
		}
	}
	return failed
}

// Create creates the declared repository and pushes its scaffold as the
// caller, and nothing else: the create step (skipped when the repository
// exists — a creation resumed after a failure) and the scaffold step (one
// commit on the default branch; skipped when it is there). The same steps
// [Run] executes for the reconciler, so the two paths cannot drift. The
// set-up that follows — settings, protection, CircleCI, the catalog — is the
// reconciler's, from the merged declaration.
//
// The caller's role in the organization is read first, in every mode: the
// organization does not let members create repositories, so anyone but an
// owner is refused with [NotOwnerRefusal] before a write ([IsNotOwner]),
// and a 403 on the creation itself ends the create step with the same text.
// A step that fails is a [VerdictFailed] in the result; the scaffold step
// then does not run and the caller reruns once the cause is fixed.
func (r *Runner) Create(ctx context.Context, req CreateRequest) (*CreateResult, error) {
	s, err := r.newRun(Request{
		Owner:         req.Owner,
		Team:          req.Team,
		Entry:         req.Entry,
		Added:         true,
		Mode:          req.Mode,
		RenderOptions: req.RenderOptions,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if s.req.Mode == ModeRepair && r.Renderer == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Renderer must not be empty: the scaffold is rendered and pushed", r)
	}

	res := &CreateResult{
		Repository: s.slug(),
		Mode:       s.req.Mode,
		StartedAt:  r.now(),
	}

	if err := r.requireOwner(ctx, s.owner); err != nil {
		return nil, microerror.Mask(err)
	}

	create := r.execute(ctx, s, StepCreate)
	res.Steps = append(res.Steps, *create)
	switch {
	case create.Verdict == VerdictFailed:
		// nothing to scaffold
	case s.repo == nil && s.req.Mode == ModeCheck:
		// The dry run of a repository that does not exist: the scaffold step
		// cannot read a repository it would create, so it plans what it
		// would push instead of reporting the repository missing.
		sr := &StepResult{Step: StepScaffold, Changes: []string{s.scaffoldChange(s.baseline.DefaultBranch)}}
		s.finish(sr)
		fmt.Fprintf(s.log, "%s/%s %s: %s%s\n", s.owner, s.name, StepScaffold, sr.Verdict, summaryLine(sr))
		res.Steps = append(res.Steps, *sr)
	default:
		res.Steps = append(res.Steps, *r.execute(ctx, s, StepScaffold))
	}

	res.Repository = s.slug()
	res.URL = s.repo.GetHTMLURL()
	res.Created = s.created
	res.ScaffoldCommit = s.scaffoldSHA
	if res.ScaffoldCommit == "" && !s.empty && s.repo != nil {
		res.ScaffoldCommit = s.headSHA
	}
	res.FinishedAt = r.now()
	return res, nil
}

// requireOwner reads the caller's membership in owner and refuses anyone
// who is not an organization owner.
func (r *Runner) requireOwner(ctx context.Context, owner string) error {
	membership, resp, err := r.GitHub.Organizations.GetOrgMembership(ctx, "", owner)
	switch {
	case isNotFound(resp, err) || isForbidden(err):
		// not a member, or a token that cannot read the membership: either
		// way GitHub will not let the caller create in the organization.
		return microerror.Maskf(notOwnerError, "%s", NotOwnerRefusal(owner))
	case err != nil:
		return microerror.Mask(fmt.Errorf("reading your membership in %s: %w", owner, err))
	case membership.GetRole() != orgRoleAdmin:
		return microerror.Maskf(notOwnerError, "%s", NotOwnerRefusal(owner))
	}
	return nil
}

// NotOwnerRefusal is the text a caller who is not an owner of the
// organization is refused with: the organization does not let members
// create repositories, and every path — this engine, the Dev Portal — creates
// the repository as the person.
func NotOwnerRefusal(owner string) string {
	return fmt.Sprintf("only an organization owner may create a repository in %s — ask an owner, or create it from the Dev Portal (which also creates it as you and needs the same role)", owner)
}

var notOwnerError = &microerror.Error{
	Kind: "notOwnerError",
}

// IsNotOwner asserts notOwnerError: the caller is not an owner of the
// organization and cannot create a repository in it.
func IsNotOwner(err error) bool {
	return microerror.Cause(err) == notOwnerError
}

// isForbidden says whether a go-github call answered 403.
func isForbidden(err error) bool {
	var ghErr *github.ErrorResponse
	return errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusForbidden
}
