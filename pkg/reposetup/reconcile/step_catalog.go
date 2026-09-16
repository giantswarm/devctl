package reconcile

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-github/v92/github"
)

// stepCatalog makes the catalog and the apps-to-teams mapping carry the
// repository: when the catalog lacks it the catalog regeneration is
// dispatched (the mapping follows once the catalog has it); when the
// catalog has it and the mapping lacks it the mapping run is dispatched
// with the repository. A run already queued or in progress is waited for,
// not dispatched again.
func (r *Runner) stepCatalog(ctx context.Context, s *run, sr *StepResult) error {
	b := s.baseline
	if b.CatalogRepository == "" || b.CatalogWorkflow == "" {
		sr.Verdict = VerdictSkipped
		sr.Summary = "no catalog in the baseline"
		return nil
	}
	catalogOwner, catalogRepo, err := splitSlug(b.CatalogRepository)
	if err != nil {
		return err
	}

	inCatalog, err := r.fileMentions(ctx, catalogOwner, catalogRepo, b.CatalogPath, s.name)
	if err != nil {
		return err
	}
	if !inCatalog {
		return r.dispatchOnce(ctx, s, sr, catalogOwner, catalogRepo, b.CatalogWorkflow,
			map[string]any{"force": true},
			fmt.Sprintf("dispatch %s in %s (force) — the catalog lacks %s; the mapping follows once the catalog carries it", b.CatalogWorkflow, b.CatalogRepository, s.name))
	}

	if b.MappingRepository == "" || b.MappingWorkflow == "" {
		sr.Summary = "in the catalog"
		return nil
	}
	mappingOwner, mappingRepo, err := splitSlug(b.MappingRepository)
	if err != nil {
		return err
	}
	inMapping, err := r.fileMentions(ctx, mappingOwner, mappingRepo, b.MappingPath, s.name)
	if err != nil {
		return err
	}
	if !inMapping {
		return r.dispatchOnce(ctx, s, sr, catalogOwner, catalogRepo, b.MappingWorkflow,
			map[string]any{"repository": s.name},
			fmt.Sprintf("dispatch %s in %s for %s — the mapping lacks it; the run opens the %s pull request, approved and auto-merged on green", b.MappingWorkflow, b.CatalogRepository, s.name, b.MappingRepository))
	}
	sr.Summary = "in the catalog and the mapping"
	return nil
}

// dispatchOnce dispatches workflow on its default branch unless a run of it
// is already queued or in progress, in which case the step stays in drift
// until that run has landed its change.
func (r *Runner) dispatchOnce(ctx context.Context, s *run, sr *StepResult, owner, repo, workflow string, inputs map[string]any, change string) error {
	runs, _, err := r.GitHub.Actions.ListWorkflowRunsByFileName(ctx, owner, repo, workflow, &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: 5},
	})
	if err != nil && !isNotFound(nil, err) {
		return err
	}
	if runs != nil {
		for _, wr := range runs.WorkflowRuns {
			switch wr.GetStatus() {
			case "queued", "in_progress", "waiting", "pending", "requested":
				sr.Verdict = VerdictDrift
				sr.Summary = fmt.Sprintf("%s run #%d %s in %s/%s; waiting for it", workflow, wr.GetRunNumber(), wr.GetStatus(), owner, repo)
				return nil
			}
		}
	}
	return s.plan(sr, change, func() error {
		_, _, err := r.GitHub.Actions.CreateWorkflowDispatchEventByFileName(ctx, owner, repo, workflow, github.CreateWorkflowDispatchEventRequest{
			Ref:    s.baseline.DefaultBranch,
			Inputs: inputs,
		})
		return err
	})
}

// fileMentions says whether the file names the repository: as a YAML value
// or key of its own (`name: <repo>`, `<repo>:`), not as a substring of
// another name.
func (r *Runner) fileMentions(ctx context.Context, owner, repo, path, name string) (bool, error) {
	data, found, err := r.fileContent(ctx, owner, repo, path, "")
	if err != nil || !found {
		return false, err
	}
	re := regexp.MustCompile(`(?m)(^|[\s"':])` + regexp.QuoteMeta(name) + `(["']?\s*:|\s*$)`)
	return re.Match(data), nil
}

func splitSlug(slug string) (owner, repo string, err error) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("baseline: %q is not owner/repo", slug)
	}
	return parts[0], parts[1], nil
}
