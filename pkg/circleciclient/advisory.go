package circleciclient

import (
	"slices"
	"strings"
)

// AdvisoryJobs are the tag-pipeline jobs whose failure does not fail a
// release: sync-china-registry mirrors the pushed image to the in-China
// registry after the release's own pushes, and nothing of the release
// requires it. devctl release wait and devctl release promote, and the
// promote step of the generated auto-release workflow, name such a failure
// as a warning; any other failed job still fails the release.
var AdvisoryJobs = []string{"sync-china-registry"}

// AdvisoryJob says whether a failed job of name leaves the release
// available.
func AdvisoryJob(name string) bool {
	return slices.Contains(AdvisoryJobs, name)
}

// StatusJobName is the job a CircleCI status context names.
func StatusJobName(context string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(context, StatusContextPrefix), ":"))
}

// AdvisoryStatus says whether a commit status context is CircleCI's for an
// advisory job: its failure leaves the release available.
func AdvisoryStatus(context string) bool {
	return strings.HasPrefix(context, StatusContextPrefix) && AdvisoryJob(StatusJobName(context))
}
