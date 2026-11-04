package prwait

import (
	"context"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// readRerunStatuses identifies failures a newer workflow will replace. A
// rerun can exist before its jobs post pending to GitHub, even before the
// rerun's jobs are listed. The previous workflow's jobs identify its contexts;
// workflow names need not be job names. Successful statuses are kept because
// rerun-from-failed reuses successful jobs without posting their statuses again.
func (s *circleSnapshot) readRerunStatuses(ctx context.Context, client *circleciclient.Client, statuses []*github.RepoStatus) error {
	statuses = latestStatuses(statuses)
	for _, newest := range latestWorkflows(s.workflows) {
		if newest.CreatedAt.IsZero() {
			continue
		}
		var stale []*github.RepoStatus
		for _, status := range statuses {
			at := status.GetUpdatedAt().Time
			if strings.HasPrefix(status.GetContext(), circleStatusPrefix) &&
				(status.GetState() == "failure" || status.GetState() == "error") &&
				!at.IsZero() && at.Before(newest.CreatedAt) {
				stale = append(stale, status)
			}
		}
		if len(stale) == 0 {
			continue
		}
		var previous *circleciclient.Workflow
		for i := range s.workflows {
			old := &s.workflows[i]
			if old.Name == newest.Name && old.ID != newest.ID && old.CreatedAt.Before(newest.CreatedAt) &&
				(previous == nil || old.CreatedAt.After(previous.CreatedAt)) {
				previous = old
			}
		}
		if previous == nil {
			continue
		}
		jobs, err := client.ListWorkflowJobs(ctx, previous.ID)
		if err != nil {
			return microerror.Mask(err)
		}
		for _, job := range jobs {
			for _, status := range stale {
				if status.GetContext() == circleStatusPrefix+job.Name {
					if s.rerunStatuses == nil {
						s.rerunStatuses = map[string]bool{}
					}
					s.rerunStatuses[status.GetContext()] = true
				}
			}
		}
	}
	return nil
}
