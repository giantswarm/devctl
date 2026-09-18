package reconcile

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// visibilityPrivate is the declaration's value for a private repository.
const visibilityPrivate = "private"

// stepCreate looks the declared repository up and creates it when it is
// missing and the entry was added by the triggering change. A redirect on
// the declared name is a rename: the run continues under the new name and
// the caller is told to open the correction PR. A missing repository the
// change did not add is reported, never created from a schedule.
func (r *Runner) stepCreate(ctx context.Context, s *run, sr *StepResult) error {
	repo, resp, err := r.GitHub.Repositories.Get(ctx, s.owner, s.name)
	switch {
	case isNotFound(resp, err):
		if s.fields.Lifecycle == LifecycleDeleted {
			// The declaration is the record of a deletion: nothing to
			// create, nothing to report.
			sr.Summary = "deleted, as declared"
			return nil
		}
		if s.req.Added && !lifecycleOver(s.fields.Lifecycle) {
			return s.plan(sr, fmt.Sprintf("create %s from the added entry", s.slug()), func() error {
				created, _, err := r.GitHub.Repositories.Create(ctx, s.owner, &github.Repository{
					Name:        new(s.name),
					Description: new(s.fields.Description),
					Private:     new(s.fields.Visibility == visibilityPrivate),
					// The initial commit lets CircleCI follow and the Git Data
					// API write; the scaffold step replaces it.
					AutoInit: new(true),
				})
				if err != nil {
					if isForbidden(err) {
						return microerror.Maskf(notOwnerError, "%s", NotOwnerRefusal(s.owner))
					}
					return err
				}
				s.repo = created
				s.created = true
				return nil
			})
		}
		fix := fmt.Sprintf("if %s was deleted, set lifecycle: deleted on the entry in repositories/%s.yaml as the record, or remove the entry; to create it, add the entry in a team-file pull request — creation happens only from the change that adds the entry, never from a schedule", s.slug(), s.req.Team)
		if s.fields.Lifecycle == LifecycleArchived {
			fix = fmt.Sprintf("%s is declared archived but does not exist: set lifecycle: deleted on the entry in repositories/%s.yaml as the record, or remove the entry", s.slug(), s.req.Team)
		}
		s.report(sr, FindingRepositoryMissing, fmt.Sprintf("%s is declared but does not exist on GitHub", s.slug()), fix)
		return nil
	case err != nil:
		return err
	}

	s.repo = repo
	if actual := repo.GetName(); !strings.EqualFold(repo.GetFullName(), s.slug()) && actual != "" {
		s.report(sr, FindingRenamed,
			fmt.Sprintf("%s redirects to %s: the repository was renamed on GitHub", s.slug(), repo.GetFullName()),
			fmt.Sprintf("open the correction pull request renaming the entry %q to %q in repositories/%s.yaml; this run set the repository up under its new name", s.declared, actual, s.req.Team))
		s.name = actual
		s.renamed = true
		s.owner = firstOr(repo.GetOwner().GetLogin(), s.owner)
	}
	sr.Summary = "exists"
	if repo.GetArchived() {
		sr.Summary = "exists, archived"
	}
	return nil
}

// stepMetadata reconciles the description and the visibility the entry
// declares; a field the entry leaves out is left alone.
func (r *Runner) stepMetadata(ctx context.Context, s *run, sr *StepResult) error {
	edit := &github.Repository{}
	var changes []string
	if want := s.fields.Description; want != "" && s.repo.GetDescription() != want {
		edit.Description = new(want)
		changes = append(changes, fmt.Sprintf("description %q → %q", s.repo.GetDescription(), want))
	}
	if want := s.fields.Visibility; want != "" {
		private := want == visibilityPrivate
		if s.repo.GetPrivate() != private {
			edit.Private = new(private)
			changes = append(changes, "visibility → "+want)
		}
	}
	if len(changes) == 0 {
		sr.Summary = "as declared"
		return nil
	}
	return s.plan(sr, strings.Join(changes, ", "), func() error {
		updated, _, err := r.GitHub.Repositories.Edit(ctx, s.owner, s.name, edit)
		if err != nil {
			return err
		}
		s.repo = updated
		return nil
	})
}

// stepLifecycle applies the lifecycle that ends a repository's life.
// lifecycle: archived — archived on GitHub, and on CircleCI unfollowed by
// the token's user and stopped from building. lifecycle: deleted — the
// CircleCI project unfollowed and stopped first (a deleted repository's
// project answers nothing afterwards), then the repository deleted on GitHub
// with its code, issues, pull requests, releases and packages; an
// organization owner can restore it on GitHub for 90 days. Either entry
// stays in the team file as the record: a later run finds a deleted
// repository gone and reports nothing. The CircleCI state read is the one
// the unfollow changes, the user's follow in the v1.1 project settings: the
// v2 project answers 200 for ever, unfollowed or stopped alike (checked live
// 2026-09-17), and reading it planned the unfollow again on every run. A
// project the token's user does not follow is left alone — an archived
// repository receives no push to build anyway. A repository archived on
// GitHub without the lifecycle is reported: the declaration is the desired
// state, and a person decides which side is right.
func (r *Runner) stepLifecycle(ctx context.Context, s *run, sr *StepResult) error {
	switch s.fields.Lifecycle {
	case LifecycleDeleted:
		return r.deleteRepository(ctx, s, sr)
	case LifecycleArchived:
		return r.archiveRepository(ctx, s, sr)
	}
	if !s.repo.GetArchived() {
		sr.Summary = "active"
		return nil
	}
	s.report(sr, FindingArchivedUndeclared,
		fmt.Sprintf("%s is archived on GitHub but the entry has no lifecycle: archived", s.slug()),
		fmt.Sprintf("set lifecycle: archived on the entry in repositories/%s.yaml if the archive is wanted, or unarchive the repository on GitHub", s.req.Team))
	return nil
}

// archiveRepository applies lifecycle: archived.
func (r *Runner) archiveRepository(ctx context.Context, s *run, sr *StepResult) error {
	if !s.repo.GetArchived() {
		err := s.plan(sr, "archive on GitHub", func() error {
			updated, _, err := r.GitHub.Repositories.Edit(ctx, s.owner, s.name, &github.Repository{Archived: new(true)})
			if err != nil {
				return err
			}
			s.repo = updated
			return nil
		})
		if err != nil {
			return err
		}
	}
	if r.CircleCI == nil {
		sr.Summary = "archived; CircleCI not checked (no client)"
		return nil
	}
	if err := r.unfollowCircleCI(ctx, s, sr); err != nil {
		return err
	}
	sr.Summary = "archived"
	return nil
}

// deleteRepository applies lifecycle: deleted: CircleCI first, then GitHub.
func (r *Runner) deleteRepository(ctx context.Context, s *run, sr *StepResult) error {
	if r.CircleCI != nil {
		if err := r.unfollowCircleCI(ctx, s, sr); err != nil {
			return err
		}
	}
	err := s.plan(sr, "delete on GitHub", func() error {
		_, err := r.GitHub.Repositories.Delete(ctx, s.owner, s.name)
		return err
	})
	if err != nil {
		return err
	}
	sr.Summary = "deleted"
	if r.CircleCI == nil {
		sr.Summary = "deleted; CircleCI not checked (no client)"
	}
	return nil
}

// unfollowCircleCI unfollows the project as the token's user and stops it
// from building, when the user follows it; a project CircleCI does not know
// is left alone.
func (r *Runner) unfollowCircleCI(ctx context.Context, s *run, sr *StepResult) error {
	following, err := r.CircleCI.Following(ctx, s.owner, s.name)
	switch {
	case circleciclient.IsNotFound(err):
		return nil // never set up on CircleCI
	case err != nil:
		return err
	}
	if !following {
		return nil
	}
	return s.plan(sr, "unfollow on CircleCI and stop building", func() error {
		if err := r.CircleCI.Unfollow(ctx, s.owner, s.name); err != nil {
			return err
		}
		if err := r.CircleCI.StopBuilding(ctx, s.owner, s.name); err != nil {
			return err
		}
		// The unfollow is done when CircleCI says so; a follow that
		// stuck is a failed step, not a repair planned again next run.
		still, err := r.CircleCI.Following(ctx, s.owner, s.name)
		if err != nil && !circleciclient.IsNotFound(err) {
			return err
		}
		if still {
			return fmt.Errorf("CircleCI still reports the token's user following %s after the unfollow", s.slug())
		}
		return nil
	})
}

// circleCIFollowed says whether CircleCI knows the project.
func (r *Runner) circleCIFollowed(ctx context.Context, s *run) (bool, error) {
	_, err := r.CircleCI.GetProject(ctx, s.owner, s.name)
	switch {
	case circleciclient.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

func firstOr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
