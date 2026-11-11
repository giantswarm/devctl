package prwait

import (
	"fmt"
	"time"
)

// RegistrationGrace is how long after the pull request's last update, which
// a push to its head and the merge commit --update-branch creates are, the
// head's workflows may still be registering their runs: GitHub delivers the
// event and creates the runs seconds after the push. Within it no verdict
// rests on what has not registered yet: a required context still absent is
// pending, not never reported, and a head without any GitHub Actions run or
// check run of its own is pending, not green.
const RegistrationGrace = time.Minute

// awaitRegistration marks a head younger than RegistrationGrace as
// registering, and an otherwise green one that carries no GitHub Actions run
// and no check run of its own as waiting for them. A check run of another app
// does not count: a gatekeeper reports before the workflows register. A head
// that registers nothing stays green once the grace has passed.
func (w *Waiter) awaitRegistration(e *evaluation) {
	grace := w.clock.Scaled(RegistrationGrace)
	e.registering = e.quietFor < grace
	if e.registering && e.green() && !e.actionsRegistered {
		e.unfinished = append(e.unfinished, fmt.Sprintf("actions runs of the head (none registered %s after the pull request's last update, grace %s)", e.quietFor.Round(time.Second), RegistrationGrace))
	}
}
