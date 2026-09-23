package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/google/go-github/v92/github"
)

// stepSettings applies the settings baseline: features, merge settings (a
// fork line keeps its rebase merges, [run.mergeMethods]), pull-request
// settings, the declared default branch and the workflows' default token
// permission. Only the fields that differ are sent.
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
	merge, err := r.mergeSettings(ctx, s)
	unchecked := err != nil
	if unchecked {
		s.report(sr, FindingUnchecked,
			fmt.Sprintf("the merge settings of %s (%s) are not readable by this identity: GET /repos/{owner}/{repo} carries them for admins only, and the GraphQL read failed: %v", s.slug(), strings.Join(mergeSettingFields, ", "), err),
			"run the check as an identity with admin rights on the repository (the reconciler's Align now), or let this identity reach GraphQL; the merge settings are compared on a later run")
	} else {
		mergeCommit, rebaseMerge := s.mergeMethods(merge)
		want("allow_merge_commit", merge.mergeCommit, mergeCommit, func(v *bool) { edit.AllowMergeCommit = v })
		want("allow_squash_merge", merge.squashMerge, b.AllowSquashMerge, func(v *bool) { edit.AllowSquashMerge = v })
		want("allow_rebase_merge", merge.rebaseMerge, rebaseMerge, func(v *bool) { edit.AllowRebaseMerge = v })
		want("allow_update_branch", merge.updateBranch, b.AllowUpdateBranch, func(v *bool) { edit.AllowUpdateBranch = v })
		want("allow_auto_merge", merge.autoMerge, b.AllowAutoMerge, func(v *bool) { edit.AllowAutoMerge = v })
		want("delete_branch_on_merge", merge.deleteBranchOnMerge, b.DeleteBranchOnMerge, func(v *bool) { edit.DeleteBranchOnMerge = v })
		if merge.squashTitle != b.SquashMergeCommitTitle {
			changes = append(changes, fmt.Sprintf("squash_merge_commit_title %s → %s", merge.squashTitle, b.SquashMergeCommitTitle))
			edit.SquashMergeCommitTitle = new(b.SquashMergeCommitTitle)
		}
	}
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

	if got, want := repo.GetDefaultBranch(), s.defaultBranch(); !s.empty && !s.keepsDefaultBranch() && got != "" && got != want {
		err := s.plan(sr, fmt.Sprintf("default branch %q → %q", got, want), func() error {
			_, _, err := r.GitHub.Repositories.RenameBranch(ctx, s.owner, s.name, got, want)
			if err != nil {
				return err
			}
			s.repo.DefaultBranch = new(want)
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
		if unchecked {
			sr.Summary = "baseline, the merge settings unchecked"
		}
	}
	return nil
}

// mergeSettingFields are the seven merge settings of GET /repos/{owner}/{repo}
// that GitHub carries only for an identity with admin rights on the
// repository. To every other identity — an App installation with
// administration: read, a member with read access, an anonymous call — the
// seven are null, which go-github's getters read as false or empty; compared
// with the baseline, the four it wants on and the squash title would read
// as drift on every repository. GraphQL's Repository carries them for any
// identity that reads the repository (checked live 2026-09-21 as an
// installation with administration: read: REST null, GraphQL the values),
// so the step reads them there when REST left them out.
var mergeSettingFields = []string{"allow_merge_commit", "allow_squash_merge", "allow_rebase_merge", "allow_update_branch", "allow_auto_merge", "delete_branch_on_merge", "squash_merge_commit_title"}

// mergeSettings are the repository's merge settings as the step compares
// them with the baseline.
type mergeSettings struct {
	mergeCommit, squashMerge, rebaseMerge, updateBranch, autoMerge, deleteBranchOnMerge bool
	// squashTitle is PR_TITLE or COMMIT_OR_PR_TITLE.
	squashTitle string
}

// mergeMethods are the merge commit and rebase merge methods the step wants
// beside the baseline's squash merge: off, as the baseline has them, except
// on a fork line. A fork line's pull requests land by rebase merge so that
// each carried patch stays one upstream-ready commit (a squash would fold a
// patch of several commits into one that cannot be sent upstream as it is),
// and a re-pin merges upstream's history: rebase merges stay on, and merge
// commits stay as the repository has them.
func (s *run) mergeMethods(got mergeSettings) (mergeCommit, rebaseMerge bool) {
	if s.hasFlavour(flavourFork) {
		return got.mergeCommit, true
	}
	return s.baseline.AllowMergeCommit, s.baseline.AllowRebaseMerge
}

// mergeSettings reads the seven merge settings: from the repository the run
// holds when GitHub returned them (an admin identity), else through GraphQL.
// An error means neither route answered; the caller reports the seven as
// unchecked, never as drift.
func (r *Runner) mergeSettings(ctx context.Context, s *run) (mergeSettings, error) {
	repo := s.repo
	if repo.AllowMergeCommit != nil && repo.AllowSquashMerge != nil && repo.AllowRebaseMerge != nil &&
		repo.AllowUpdateBranch != nil && repo.AllowAutoMerge != nil && repo.DeleteBranchOnMerge != nil &&
		repo.SquashMergeCommitTitle != nil {
		return mergeSettings{
			mergeCommit: repo.GetAllowMergeCommit(), squashMerge: repo.GetAllowSquashMerge(), rebaseMerge: repo.GetAllowRebaseMerge(),
			updateBranch: repo.GetAllowUpdateBranch(), autoMerge: repo.GetAllowAutoMerge(), deleteBranchOnMerge: repo.GetDeleteBranchOnMerge(),
			squashTitle: repo.GetSquashMergeCommitTitle(),
		}, nil
	}
	fmt.Fprintf(s.log, "%s/%s settings: the merge settings are not in the repository for this identity (admins only); reading them through GraphQL\n", s.owner, s.name)
	return r.mergeSettingsGraphQL(ctx, s)
}

// mergeSettingsGraphQL reads the seven merge settings from GraphQL's
// Repository, at <REST root>/graphql with the GitHub client's own transport
// and identity.
func (r *Runner) mergeSettingsGraphQL(ctx context.Context, s *run) (mergeSettings, error) {
	body, err := json.Marshal(map[string]any{
		"query":     `query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed allowUpdateBranch autoMergeAllowed deleteBranchOnMerge squashMergeCommitTitle } }`,
		"variables": map[string]string{"owner": s.owner, "name": s.name},
	})
	if err != nil {
		return mergeSettings{}, err
	}
	url := strings.TrimRight(r.GitHub.BaseURL(), "/") + "/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return mergeSettings{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := r.GitHub.Client().Do(req)
	if err != nil {
		return mergeSettings{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return mergeSettings{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return mergeSettings{}, fmt.Errorf("GraphQL answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var answer struct {
		Data struct {
			Repository *struct {
				MergeCommitAllowed     bool   `json:"mergeCommitAllowed"`
				SquashMergeAllowed     bool   `json:"squashMergeAllowed"`
				RebaseMergeAllowed     bool   `json:"rebaseMergeAllowed"`
				AllowUpdateBranch      bool   `json:"allowUpdateBranch"`
				AutoMergeAllowed       bool   `json:"autoMergeAllowed"`
				DeleteBranchOnMerge    bool   `json:"deleteBranchOnMerge"`
				SquashMergeCommitTitle string `json:"squashMergeCommitTitle"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return mergeSettings{}, fmt.Errorf("GraphQL answered with no JSON: %s", strings.TrimSpace(string(raw)))
	}
	if got := answer.Data.Repository; got != nil {
		// The schema declares the enum non-null; an answer without it is
		// an identity the field is hidden from, unchecked rather than
		// drift from an empty value.
		if got.SquashMergeCommitTitle == "" {
			return mergeSettings{}, fmt.Errorf("GraphQL answered without squashMergeCommitTitle")
		}
		return mergeSettings{
			mergeCommit: got.MergeCommitAllowed, squashMerge: got.SquashMergeAllowed, rebaseMerge: got.RebaseMergeAllowed,
			updateBranch: got.AllowUpdateBranch, autoMerge: got.AutoMergeAllowed, deleteBranchOnMerge: got.DeleteBranchOnMerge,
			squashTitle: got.SquashMergeCommitTitle,
		}, nil
	}
	messages := make([]string, 0, len(answer.Errors))
	for _, e := range answer.Errors {
		messages = append(messages, e.Message)
	}
	if len(messages) == 0 {
		messages = append(messages, "no repository in the answer")
	}
	return mergeSettings{}, fmt.Errorf("GraphQL: %s", strings.Join(messages, "; "))
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

// readHooks lists the repository's webhooks once per run: the circleci step
// verifies CircleCI's among them, the webhooks step ensures the baseline's.
func (r *Runner) readHooks(ctx context.Context, s *run) ([]*github.Hook, *github.Response, error) {
	if !s.hooksRead {
		s.hooks, s.hooksResp, s.hooksErr = r.GitHub.Repositories.ListHooks(ctx, s.owner, s.name, &github.ListOptions{PerPage: 100})
		s.hooksRead = true
	}
	return s.hooks, s.hooksResp, s.hooksErr
}

// stepWebhooks ensures the baseline's webhooks, matched by URL.
func (r *Runner) stepWebhooks(ctx context.Context, s *run, sr *StepResult) error {
	if len(s.baseline.Webhooks) == 0 {
		sr.Summary = "none in the baseline"
		return nil
	}
	hooks, _, err := r.readHooks(ctx, s)
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
