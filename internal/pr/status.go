package pr

import (
	"sync"
	"time"
)

// PRStatus tracks the status of a PR during processing.
type PRStatus struct {
	Number     int
	Owner      string
	Repo       string
	Title      string
	URL        string
	Status     string
	LastUpdate time.Time
	mu         sync.Mutex
}

// UpdateStatus updates the status of a PR in a thread-safe manner.
func (ps *PRStatus) UpdateStatus(status string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.Status = status
	ps.LastUpdate = time.Now()
}

// GetStatus returns the current status of a PR in a thread-safe manner.
func (ps *PRStatus) GetStatus() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.Status
}

