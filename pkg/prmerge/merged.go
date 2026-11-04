package prmerge

import "slices"

// mergedPastReview replaces the wait's missing-approval diagnostics once
// GitHub confirms the merge, even if branch cleanup or the release fails.
// A queued merge may have gained its approval while queued; only a direct
// merge is described as using the ruleset bypass.
func (m *Merger) mergedPastReview(result *Result) {
	const missing = "an approving review the base requires is missing"
	const blocking = "review: " + missing
	if !slices.Contains(result.Blocking, blocking) {
		return
	}
	result.Blocking = slices.DeleteFunc(result.Blocking, func(s string) bool { return s == blocking })
	warning := "merged through the ruleset bypass: " + missing
	if result.Enqueued {
		warning = "merged through the merge queue: the wait reported a missing required approval"
	}
	for i, w := range result.Warnings {
		if w == "GitHub blocks the merge: "+missing {
			result.Warnings[i] = warning
		}
	}
	m.progress.Printf("warning: %s", warning)
}
