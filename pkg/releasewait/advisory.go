package releasewait

import "slices"

// advisoryJobs are the tag-pipeline jobs whose failure does not fail the
// release: sync-china-registry mirrors the pushed image to the in-China
// registry after the release's own pushes, and nothing of the release
// requires it. Its failure is named as a warning; any other failed job still
// fails the wait.
var advisoryJobs = []string{"sync-china-registry"}

// advisoryJob says whether a failed job of name leaves the release
// available.
func advisoryJob(name string) bool {
	return slices.Contains(advisoryJobs, name)
}

// warnOnce warns message the first time the wait meets it: the tag's CI is
// read on every poll.
func (w *Waiter) warnOnce(message string) {
	if w.warned == nil {
		w.warned = map[string]bool{}
	}
	if w.warned[message] {
		return
	}
	w.warned[message] = true
	w.warn(message)
}
