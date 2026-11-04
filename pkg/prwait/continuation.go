package prwait

import (
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// missingContinuation describes a completed setup with no observed
// continuation. Only the wait's deadline gives this observation its bound;
// until then a late continuation must still be allowed to appear.
func missingContinuation(c *circleSnapshot, statuses int) string {
	if c == nil || c.pipeline == nil || statuses > 1 {
		return ""
	}
	if !circleciclient.PipelineContinuing(c.pipeline.State) && c.pipeline.State != "created" {
		return ""
	}
	workflows := latestWorkflows(c.workflows)
	if !circleciclient.SetupOnly(workflows) {
		return ""
	}
	for _, workflow := range workflows {
		if !circleciclient.WorkflowSucceeded(workflow.Status) {
			return ""
		}
	}
	return fmt.Sprintf("CircleCI continuation missing: pipeline %d exists and its setup succeeded, but no continuation workflow appeared before the deadline", c.pipeline.Number)
}
