package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-github/v92/github"
	"gopkg.in/yaml.v3"
)

// stepCatalog makes the catalog and the apps-to-teams mapping carry the
// repository: when the catalog lacks it the catalog regeneration is
// dispatched (the mapping follows once the catalog has it); when the
// catalog has it and the mapping lacks one of the component's public charts
// (the helmcharts annotation, matched by chart name — chartName overrides
// and -app suffixes differ from the repository) the mapping run is
// dispatched with the repository. A component without a public chart has
// nothing to map. A run already queued or in progress is waited for, not
// dispatched again.
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

	catalog, found, err := r.fileContent(ctx, catalogOwner, catalogRepo, b.CatalogPath, "")
	if err != nil {
		return err
	}
	component, inCatalog := findComponent(catalog, s.name)
	if !found || !inCatalog {
		return r.dispatchOnce(ctx, s, sr, catalogOwner, catalogRepo, b.CatalogWorkflow,
			map[string]any{"force": true},
			fmt.Sprintf("dispatch %s in %s (force) — the catalog lacks %s; the mapping follows once the catalog carries it", b.CatalogWorkflow, b.CatalogRepository, s.name))
	}

	if b.MappingRepository == "" || b.MappingWorkflow == "" {
		sr.Summary = "in the catalog"
		return nil
	}
	// The mapping lists public deployable charts: a repository without one
	// (a Go service without a chart, a library, a CLI) has nothing to map,
	// and the mapping run would answer with a notice on every dispatch.
	charts := component.publicCharts()
	if len(charts) == 0 {
		sr.Summary = "in the catalog; no public chart to map"
		return nil
	}
	mappingOwner, mappingRepo, err := splitSlug(b.MappingRepository)
	if err != nil {
		return err
	}
	mapping, _, err := r.fileContent(ctx, mappingOwner, mappingRepo, b.MappingPath, "")
	if err != nil {
		return err
	}
	var missing []string
	for _, chart := range charts {
		if !fileMentions(mapping, chart) {
			missing = append(missing, chart)
		}
	}
	if len(missing) > 0 {
		return r.dispatchOnce(ctx, s, sr, catalogOwner, catalogRepo, b.MappingWorkflow,
			map[string]any{"repository": s.name},
			fmt.Sprintf("dispatch %s in %s for %s — the mapping lacks %s; the run opens the %s pull request, approved and auto-merged on green", b.MappingWorkflow, b.CatalogRepository, s.name, describe(missing), b.MappingRepository))
	}
	sr.Summary = fmt.Sprintf("in the catalog and the mapping (%s)", describe(charts))
	return nil
}

// helmChartsAnnotation names the component's charts as registry references
// (gsoci.azurecr.io/charts/giantswarm/<chart>), comma-separated.
const helmChartsAnnotation = "giantswarm.io/helmcharts"

// privateRegistryPrefix marks a chart reference the mapping never lists.
const privateRegistryPrefix = "gsociprivate."

// component is the catalog's Backstage Component of a repository.
type component struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name        string            `yaml:"name"`
		Annotations map[string]string `yaml:"annotations"`
	} `yaml:"metadata"`
}

// publicCharts returns the names of the component's charts on a public
// registry, from the helmcharts annotation; none without the annotation.
func (c component) publicCharts() []string {
	var charts []string
	for _, ref := range strings.Split(c.Metadata.Annotations[helmChartsAnnotation], ",") {
		ref = strings.TrimSpace(ref)
		if ref == "" || strings.HasPrefix(ref, privateRegistryPrefix) {
			continue
		}
		charts = append(charts, ref[strings.LastIndexByte(ref, '/')+1:])
	}
	return charts
}

// findComponent returns the Component named name among the catalog's YAML
// documents; false when none is.
func findComponent(catalog []byte, name string) (component, bool) {
	dec := yaml.NewDecoder(bytes.NewReader(catalog))
	for {
		var c component
		if err := dec.Decode(&c); err != nil {
			return component{}, false // io.EOF, or a document the catalog generator would not have written
		}
		if c.Kind == "Component" && c.Metadata.Name == name {
			return c, true
		}
	}
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

// fileMentions says whether the YAML data names name as a value or key of
// its own (`name: <name>`, `<name>:`), not as a substring of another name.
func fileMentions(data []byte, name string) bool {
	re := regexp.MustCompile(`(?m)(^|[\s"':])` + regexp.QuoteMeta(name) + `(["']?\s*:|\s*$)`)
	return re.Match(data)
}

func splitSlug(slug string) (owner, repo string, err error) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("baseline: %q is not owner/repo", slug)
	}
	return parts[0], parts[1], nil
}
