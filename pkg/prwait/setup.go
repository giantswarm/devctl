package prwait

import (
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// setupHaltGrace lets a successful setup continue its pipeline before a
// pipeline still in setup can be judged halted. A submitted continuation
// moves the pipeline to pending, then created; neither state is eligible.
const setupHaltGrace = 5 * time.Minute

// haltCandidate requires every current setup workflow to have succeeded
// at least setupHaltGrace ago. Missing timestamps or a lagging workflow
// listing cannot establish that setup has finished.
func (c *circleSnapshot) haltCandidate(now time.Time) bool {
	if c.pipeline == nil || c.pipeline.State != "setup" {
		return false
	}
	workflows := latestWorkflows(c.workflows)
	if !circleciclient.SetupOnly(workflows) {
		return false
	}
	for _, w := range workflows {
		if !circleciclient.WorkflowSucceeded(w.Status) || w.StoppedAt.IsZero() || now.Sub(w.StoppedAt) < setupHaltGrace {
			return false
		}
	}
	return true
}

// setupHalted confirms that the head's CircleCI contexts are the successful
// setup jobs' own contexts. A workflow's name need not name its job, and an
// extra context is evidence of a continuation the listing may not show yet.
func (c *circleSnapshot) setupHalted(now time.Time, statuses []*github.RepoStatus) bool {
	if !c.haltCandidate(now) {
		return false
	}
	reported := map[string]string{}
	for _, s := range latestStatuses(statuses) {
		reported[s.GetContext()] = s.GetState()
	}
	setupContexts := map[string]bool{}
	for _, w := range latestWorkflows(c.workflows) {
		jobs := c.setupJobs[w.ID]
		if len(jobs) == 0 {
			return false
		}
		for _, job := range jobs {
			name := circleStatusPrefix + job.Name
			if job.Name == "" || job.Status != conclusionSuccess || reported[name] != conclusionSuccess {
				return false
			}
			setupContexts[name] = true
		}
	}
	return circleStatuses(statuses) == len(setupContexts)
}
