package reconcile

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Requests is what a run cost in requests to each system: every request the
// run's clients sent, counted at the transport ([Counter]), whatever it
// answered. The result carries it so that the cost of a repository is known
// against the reconciler's budget, the App's requests per hour.
type Requests struct {
	GitHub   int `json:"github"`
	CircleCI int `json:"circleci"`
}

// since is the cost since an earlier reading of the same counters.
func (q Requests) since(before Requests) Requests {
	return Requests{GitHub: q.GitHub - before.GitHub, CircleCI: q.CircleCI - before.CircleCI}
}

// String renders the cost for a person; "" when nothing was counted.
func (q Requests) String() string {
	if q == (Requests{}) {
		return ""
	}
	return fmt.Sprintf("%d GitHub and %d CircleCI requests", q.GitHub, q.CircleCI)
}

// Counter is an http.RoundTripper that counts the requests it sends. The
// caller builds a client's transport over one and hands it to the [Runner],
// which reads the run's cost from it ([Result.Requests]). A request counts
// when it is sent, whatever it answers; one Counter may sit under several
// clients of the same system, so their requests add up.
type Counter struct {
	// Base sends the requests; nil means http.DefaultTransport.
	Base http.RoundTripper

	n atomic.Int64
}

// RoundTrip implements http.RoundTripper.
func (c *Counter) RoundTrip(req *http.Request) (*http.Response, error) {
	c.n.Add(1)
	base := c.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// Count is how many requests were sent through the Counter so far.
func (c *Counter) Count() int {
	return int(c.n.Load())
}

// count is c's Count, 0 for no Counter.
func count(c *Counter) int {
	if c == nil {
		return 0
	}
	return c.Count()
}
