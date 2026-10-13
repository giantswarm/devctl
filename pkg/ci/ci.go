// Package ci is what `devctl ci jobs` and `devctl ci rerun` share: the
// job-level view of a CircleCI pipeline, which tells a slow job (its step
// still writing output) from a stuck one (no output for an hour), and the
// rerun of one workflow, once an hour, with the CircleCI login of `devctl
// auth login`. GitHub is not read: a pipeline or a workflow is named by what
// CircleCI calls it, the number or id the UI and `devctl pr wait` show.
package ci

import (
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// The commands, as their documents name them.
const (
	CommandJobs  = "ci jobs"
	CommandRerun = "ci rerun"
)

// Pipeline is the CircleCI pipeline of a document.
type Pipeline struct {
	ID        string     `json:"id"`
	Number    int64      `json:"number"`
	State     string     `json:"state,omitempty"`
	URL       string     `json:"url"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	Branch    string     `json:"branch,omitempty"`
	Tag       string     `json:"tag,omitempty"`
	Revision  string     `json:"revision,omitempty"`
}

// Run is one workflow run. A rerun is a second run of the same name in the
// same pipeline; the newest run of a name says where the pipeline stands.
type Run struct {
	Name      string     `json:"name"`
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"createdAt"`
	StoppedAt *time.Time `json:"stoppedAt,omitempty"`
}

func newPipeline(org, repo string, p circleciclient.Pipeline) *Pipeline {
	return &Pipeline{
		ID:        p.ID,
		Number:    p.Number,
		State:     p.State,
		URL:       circleciclient.PipelineURL(org, repo, p.Number),
		CreatedAt: timePtr(p.CreatedAt),
		Branch:    p.VCS.Branch,
		Tag:       p.VCS.Tag,
		Revision:  p.VCS.Revision,
	}
}

func newRun(org, repo string, number int64, w circleciclient.Workflow) Run {
	return Run{
		Name:      w.Name,
		ID:        w.ID,
		Status:    w.Status,
		URL:       circleciclient.WorkflowURL(org, repo, number, w.ID),
		CreatedAt: w.CreatedAt.UTC(),
		StoppedAt: timePtr(w.StoppedAt),
	}
}

// sameProject says whether a project slug CircleCI answered names org/repo.
// An answer without a slug contradicts nothing.
func sameProject(slug, org, repo string) bool {
	return slug == "" || strings.EqualFold(slug, circleciclient.ProjectSlug(org, repo))
}

// timePtr is t for the document: absent when zero.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

// seconds is d in whole seconds, never negative: a clock that runs behind
// CircleCI's reads as no time at all, not as time travel.
func seconds(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return int64(d / time.Second)
}

func notApplicable(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, format, args...)
}

func refused(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused, format, args...)
}
