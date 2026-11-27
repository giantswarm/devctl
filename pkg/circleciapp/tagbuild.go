package circleciapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
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

// oauthBuilt says an OAuth status on tag's commit, posted since the release,
// comes from a pipeline that checked out tag. A status links its job, by
// workflow (https://app.circleci.com/workflow/<id>/job/<id>, as a follow
// posts) or by project and number (https://circleci.com/gh/<org>/<repo>/<n>);
// the workflow or the job names its pipeline and the pipeline its ref. The
// default branch build of the follow that created the project lands on the
// commit a first release shares, and is no build of the tag, also in a later
// run. identified is false when a status's pipeline cannot be read — no job
// URL, or CircleCI shows the person no such job, workflow or ref in this
// project — and then oauthBuilt is true: what may have been published is
// not published again.
func (r *tagBuildReader) oauthBuilt(ctx context.Context, org, repo, tag string, statuses []*github.RepoStatus, published time.Time) (built, identified bool, err error) {
	identified = true
	for _, st := range statuses {
		if login := st.GetCreator().GetLogin(); login == reconcile.CircleCIAppLogin || !reconcile.OAuthBuiltSince([]*github.RepoStatus{st}, published) {
			continue
		}
		checkout, ok, err := r.oauthCheckout(ctx, org, repo, st.GetTargetURL())
		switch {
		case err != nil:
			return false, false, err
		case !ok:
			identified = false
		case checkout == tag:
			return true, true, nil
		}
	}
	return !identified, identified, nil
}

// oauthCheckout is the tag the pipeline of an OAuth status's job checked
// out, empty for a branch; ok is false when the target leads to no pipeline
// of the project the person sees.
func (r *tagBuildReader) oauthCheckout(ctx context.Context, org, repo, target string) (checkout string, ok bool, err error) {
	if id := reconcile.StatusWorkflow(target); id != "" && id != target {
		if checkout, ok := r.workflows[id]; ok {
			return checkout, true, nil
		}
		w, err := r.ci.GetWorkflow(ctx, id)
		switch {
		case circleciclient.IsNotFound(err):
			return "", false, nil
		case err != nil:
			return "", false, fmt.Errorf("reading workflow %s: %w", id, err)
		case w == nil || w.PipelineID == "" || w.ProjectSlug != r.project:
			return "", false, nil
		}
		c, found, err := r.pipelineCheckout(ctx, w.PipelineID)
		if found {
			r.workflows[id] = c
		}
		return c, found, err
	}
	t, found := circleciclient.ParseStatusTarget(target)
	// A job of another project — a renamed or moved repository — is
	// not looked up by its number here.
	if !found || t.Job == 0 || !strings.EqualFold(t.Org, org) || !strings.EqualFold(t.Repo, repo) {
		return "", false, nil
	}
	job, err := r.ci.GetProjectJob(ctx, org, repo, t.Job)
	switch {
	case circleciclient.IsNotFound(err):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("reading job %d: %w", t.Job, err)
	case job.Pipeline.ID == "":
		return "", false, nil
	}
	return r.pipelineCheckout(ctx, job.Pipeline.ID)
}

// pipelineCheckout is the tag pipelineID checked out, empty for a branch; ok
// is false for a pipeline of another project, without a ref or unknown.
func (r *tagBuildReader) pipelineCheckout(ctx context.Context, pipelineID string) (checkout string, ok bool, err error) {
	if checkout, ok := r.pipelines[pipelineID]; ok {
		return checkout, true, nil
	}
	p, err := r.ci.GetPipeline(ctx, pipelineID)
	switch {
	case circleciclient.IsNotFound(err):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("reading pipeline %s: %w", pipelineID, err)
	case p == nil || p.ProjectSlug != r.project || (p.VCS.Tag == "" && p.VCS.Branch == ""):
		return "", false, nil
	}
	r.pipelines[pipelineID] = p.VCS.Tag
	return p.VCS.Tag, true, nil
}
