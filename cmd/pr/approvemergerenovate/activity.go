package approvemergerenovate

import "sync"

// activity commits normal completion only when neither PR workers nor a
// discovery read can add more work. All WaitGroup additions precede that
// decision; the group is joined only after the poller has stopped.
type activity struct {
	mu      sync.Mutex
	pending int
	polling bool
	watch   bool
	ended   bool
	done    chan struct{}
}

func newActivity(pending int, watch bool) *activity {
	a := &activity{pending: pending, watch: watch, done: make(chan struct{})}
	a.finish()
	return a
}

func (a *activity) beginPoll() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ended {
		return false
	}
	a.polling = true
	return true
}

func (a *activity) endPoll(added int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending += added
	a.polling = false
	a.finish()
}

func (a *activity) finishPR() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending--
	a.finish()
}

// finish runs with mu held, or during construction before a is shared.
func (a *activity) finish() {
	if !a.watch && !a.ended && a.pending == 0 && !a.polling {
		a.ended = true
		close(a.done)
	}
}
