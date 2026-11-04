package circleciapp

import (
	"context"
	"fmt"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// tagBuildReader attributes commit statuses to checkout tags, caching the
// reads shared by releases on the same commit. A started build counts even
// when pending or failed: Setup must not publish its tag a second time.
type tagBuildReader struct {
	ci        CircleCI
	project   string
	workflows map[string]string
	pipelines map[string]string
}

func (r *tagBuildReader) built(ctx context.Context, tag string, statuses []*github.RepoStatus) (bool, error) {
	// Read every workflow, rather than the newest status of each context:
	// another image's pipeline can have posted the same contexts later.
	for _, st := range statuses {
		id := reconcile.StatusWorkflow(st.GetTargetURL())
		if id == "" {
			return false, fmt.Errorf("CircleCI status %q has no workflow URL", st.GetContext())
		}
		checkout, ok := r.workflows[id]
		if !ok {
			w, err := r.ci.GetWorkflow(ctx, id)
			if err != nil {
				return false, fmt.Errorf("reading workflow %s: %w", id, err)
			}
			if w == nil || w.PipelineID == "" || w.ProjectSlug != r.project {
				return false, fmt.Errorf("workflow %s does not identify a pipeline in %s", id, r.project)
			}
			checkout, ok = r.pipelines[w.PipelineID]
			if !ok {
				p, err := r.ci.GetPipeline(ctx, w.PipelineID)
				if err != nil {
					return false, fmt.Errorf("reading pipeline %s: %w", w.PipelineID, err)
				}
				if p == nil || p.ProjectSlug != r.project || (p.VCS.Tag == "" && p.VCS.Branch == "") {
					return false, fmt.Errorf("pipeline %s does not identify its checkout ref in %s", w.PipelineID, r.project)
				}
				checkout = p.VCS.Tag
				r.pipelines[w.PipelineID] = checkout
			}
			r.workflows[id] = checkout
		}
		if checkout == tag {
			return true, nil
		}
	}
	return false, nil
}
