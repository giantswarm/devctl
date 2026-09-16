package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-github/v92/github"
)

// stepSettings applies the settings baseline: features, merge settings,
// pull-request settings, the default branch and the workflows' default
// token permission. Only the fields that differ are sent.
func (r *Runner) stepSettings(ctx context.Context, s *run, sr *StepResult) error {
	b := s.baseline
	edit := &github.Repository{}
	var changes []string
	want := func(field string, got, want bool, set func(*bool)) {
		if got != want {
			changes = append(changes, fmt.Sprintf("%s %t → %t", field, got, want))
			set(&want)
		}
	}
	repo := s.repo
	want("has_wiki", repo.GetHasWiki(), b.HasWiki, func(v *bool) { edit.HasWiki = v })
	want("has_issues", repo.GetHasIssues(), b.HasIssues, func(v *bool) { edit.HasIssues = v })
	want("has_projects", repo.GetHasProjects(), b.HasProjects, func(v *bool) { edit.HasProjects = v })
	want("allow_merge_commit", repo.GetAllowMergeCommit(), b.AllowMergeCommit, func(v *bool) { edit.AllowMergeCommit = v })
	want("allow_squash_merge", repo.GetAllowSquashMerge(), b.AllowSquashMerge, func(v *bool) { edit.AllowSquashMerge = v })
	want("allow_rebase_merge", repo.GetAllowRebaseMerge(), b.AllowRebaseMerge, func(v *bool) { edit.AllowRebaseMerge = v })
	want("allow_update_branch", repo.GetAllowUpdateBranch(), b.AllowUpdateBranch, func(v *bool) { edit.AllowUpdateBranch = v })
	want("allow_auto_merge", repo.GetAllowAutoMerge(), b.AllowAutoMerge, func(v *bool) { edit.AllowAutoMerge = v })
	want("delete_branch_on_merge", repo.GetDeleteBranchOnMerge(), b.DeleteBranchOnMerge, func(v *bool) { edit.DeleteBranchOnMerge = v })
	if len(changes) > 0 {
		err := s.plan(sr, "settings: "+strings.Join(changes, ", "), func() error {
			updated, _, err := r.GitHub.Repositories.Edit(ctx, s.owner, s.name, edit)
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

	if got := repo.GetDefaultBranch(); !s.empty && got != "" && got != b.DefaultBranch {
		err := s.plan(sr, fmt.Sprintf("default branch %q → %q", got, b.DefaultBranch), func() error {
			_, _, err := r.GitHub.Repositories.RenameBranch(ctx, s.owner, s.name, got, b.DefaultBranch)
			if err != nil {
				return err
			}
			s.repo.DefaultBranch = new(b.DefaultBranch)
			return nil
		})
		if err != nil {
			return err
		}
	}

	perms, _, err := r.GitHub.Repositories.GetDefaultWorkflowPermissions(ctx, s.owner, s.name)
	if err != nil {
		return err
	}
	if got := perms.GetDefaultWorkflowPermissions(); got != b.WorkflowPermissions {
		err := s.plan(sr, fmt.Sprintf("default workflow permissions %q → %q", got, b.WorkflowPermissions), func() error {
			_, _, err := r.GitHub.Repositories.UpdateDefaultWorkflowPermissions(ctx, s.owner, s.name, github.DefaultWorkflowPermissionRepository{
				DefaultWorkflowPermissions: new(b.WorkflowPermissions),
			})
			return err
		})
		if err != nil {
			return err
		}
	}
	if len(sr.Changes) == 0 {
		sr.Summary = "baseline"
	}
	return nil
}

// stepPermissions grants the baseline's teams their permission; teams the
// baseline does not name keep whatever they have.
func (r *Runner) stepPermissions(ctx context.Context, s *run, sr *StepResult) error {
	teams, _, err := r.GitHub.Repositories.ListTeams(ctx, s.owner, s.name, &github.ListOptions{PerPage: 100})
	if err != nil {
		return err
	}
	have := make(map[string]string, len(teams))
	for _, t := range teams {
		have[strings.ToLower(t.GetSlug())] = t.GetPermission()
	}
	slugs := make([]string, 0, len(s.baseline.TeamPermissions))
	for slug := range s.baseline.TeamPermissions {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		want := s.baseline.TeamPermissions[slug]
		if have[strings.ToLower(slug)] == want {
			continue
		}
		err := s.plan(sr, fmt.Sprintf("team %s: %s", slug, want), func() error {
			_, err := r.GitHub.Teams.AddTeamRepoBySlug(ctx, s.owner, slug, s.owner, s.name, &github.TeamAddTeamRepoOptions{Permission: want})
			return err
		})
		if err != nil {
			return err
		}
	}
	if len(sr.Changes) == 0 {
		sr.Summary = "baseline"
	}
	return nil
}

// stepWebhooks ensures the baseline's webhooks, matched by URL.
func (r *Runner) stepWebhooks(ctx context.Context, s *run, sr *StepResult) error {
	if len(s.baseline.Webhooks) == 0 {
		sr.Summary = "none in the baseline"
		return nil
	}
	hooks, _, err := r.GitHub.Repositories.ListHooks(ctx, s.owner, s.name, &github.ListOptions{PerPage: 100})
	if err != nil {
		return err
	}
	for _, want := range s.baseline.Webhooks {
		var existing *github.Hook
		for _, h := range hooks {
			if h.GetConfig().GetURL() == want.URL {
				existing = h
				break
			}
		}
		hook := &github.Hook{
			Name:   new("web"),
			Active: new(true),
			Events: want.Events,
			Config: &github.HookConfig{
				URL:         new(want.URL),
				ContentType: new(want.ContentType),
				InsecureSSL: new("0"),
			},
		}
		if want.Secret != "" {
			hook.Config.Secret = new(want.Secret)
		}
		switch {
		case existing == nil:
			err = s.plan(sr, "create webhook "+want.URL, func() error {
				_, _, err := r.GitHub.Repositories.CreateHook(ctx, s.owner, s.name, hook)
				return err
			})
		case !existing.GetActive() || !sameSet(existing.Events, want.Events) || existing.GetConfig().GetContentType() != want.ContentType:
			err = s.plan(sr, "update webhook "+want.URL, func() error {
				_, _, err := r.GitHub.Repositories.EditHook(ctx, s.owner, s.name, existing.GetID(), hook)
				return err
			})
		}
		if err != nil {
			return err
		}
	}
	if len(sr.Changes) == 0 {
		sr.Summary = "present"
	}
	return nil
}

// installationRepositories is a page of GET /user/installations/{id}/repositories.
type installationRepositories struct {
	TotalCount          int    `json:"total_count"`
	RepositorySelection string `json:"repository_selection"`
	Repositories        []struct {
		FullName string `json:"full_name"`
	} `json:"repositories"`
}

// stepRenovate checks that the Renovate installation covers the repository.
// It never writes: adding a repository to the installation is an org
// owner's click, and the installation is to cover every repository.
func (r *Runner) stepRenovate(ctx context.Context, s *run, sr *StepResult) error {
	id := s.baseline.RenovateInstallationID
	if id == 0 {
		sr.Verdict = VerdictSkipped
		sr.Summary = "no Renovate installation in the baseline"
		return nil
	}
	installationURL := fmt.Sprintf("https://github.com/organizations/%s/settings/installations/%d", s.owner, id)
	for page := 1; ; page++ {
		req, err := r.GitHub.NewRequest(ctx, "GET", fmt.Sprintf("user/installations/%d/repositories?per_page=100&page=%d", id, page), nil)
		if err != nil {
			return err
		}
		var repos installationRepositories
		resp, err := r.GitHub.Do(req, &repos)
		if err != nil {
			if resp != nil && resp.Response != nil && (resp.StatusCode == 403 || resp.StatusCode == 404) {
				sr.Verdict = VerdictSkipped
				sr.Summary = fmt.Sprintf("cannot read the Renovate installation with this token (HTTP %d): a GitHub App token cannot list a user's installations", resp.StatusCode)
				return nil
			}
			return err
		}
		if repos.RepositorySelection == "all" {
			sr.Summary = "installation covers all repositories"
			return nil
		}
		for _, repo := range repos.Repositories {
			if strings.EqualFold(repo.FullName, s.slug()) {
				sr.Summary = "installation covers the repository"
				return nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
	}
	s.report(sr, FindingRenovateMissing,
		fmt.Sprintf("the Renovate installation does not cover %s: no dependency updates arrive", s.slug()),
		fmt.Sprintf("an organization owner adds the repository at %s, or switches the installation to all repositories", installationURL))
	return nil
}
