package releasewait

// warnOnce warns message the first time the wait meets it: the tag's CI is
// read on every poll. A failed advisory job
// ([circleciclient.AdvisoryJob]) is named this way and does not fail the
// wait.
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
