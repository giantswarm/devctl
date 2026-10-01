package githubclient

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Conditional is an http.RoundTripper that makes every GET a conditional
// request: it keeps the ETag and body of the last 200 per URL, sends the tag
// as If-None-Match and replays the kept body when GitHub answers 304 Not
// Modified. A 304 does not count against the rate limit, so a poll loop
// that sees no change costs nothing; go-github never sees the 304, it reads a
// 200 with the same body as before.
//
// It also remembers the rate-limit headers of the newest answer, 304 or not,
// so a poll loop derives its interval from real responses and never asks the
// rate_limit endpoint.
type Conditional struct {
	// Base sends the requests; nil means http.DefaultTransport.
	Base http.RoundTripper

	mu       sync.Mutex
	cache    map[string]conditionalEntry
	rate     RateLimit
	replayed int
}

type conditionalEntry struct {
	etag   string
	header http.Header
	body   []byte
}

// RateLimit is what the newest response said about the budget.
type RateLimit struct {
	// Known is false before the first response with rate-limit headers.
	Known     bool
	Remaining int
	Reset     time.Time
}

// RoundTrip implements http.RoundTripper.
func (c *Conditional) RoundTrip(req *http.Request) (*http.Response, error) {
	base := c.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.Method != http.MethodGet {
		resp, err := base.RoundTrip(req)
		if err == nil {
			c.observe(resp.Header)
		}
		return resp, err
	}

	key := req.URL.String()
	c.mu.Lock()
	entry, cached := c.cache[key]
	c.mu.Unlock()
	if cached {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", entry.etag)
	}

	resp, err := base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	c.observe(resp.Header)

	switch {
	case resp.StatusCode == http.StatusNotModified && cached:
		_ = resp.Body.Close()
		replay := &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Proto:         resp.Proto,
			ProtoMajor:    resp.ProtoMajor,
			ProtoMinor:    resp.ProtoMinor,
			Header:        entry.header.Clone(),
			Body:          io.NopCloser(bytes.NewReader(entry.body)),
			ContentLength: int64(len(entry.body)),
			Request:       req,
		}
		// The budget headers are the 304's: they are the fresh ones.
		for name, values := range resp.Header {
			if strings.HasPrefix(strings.ToLower(name), "x-ratelimit-") {
				replay.Header[name] = values
			}
		}
		c.mu.Lock()
		c.replayed++
		c.mu.Unlock()
		return replay, nil
	case resp.StatusCode == http.StatusOK && resp.Header.Get("ETag") != "":
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		c.mu.Lock()
		if c.cache == nil {
			c.cache = map[string]conditionalEntry{}
		}
		c.cache[key] = conditionalEntry{etag: resp.Header.Get("ETag"), header: resp.Header.Clone(), body: body}
		c.mu.Unlock()
	}
	return resp, nil
}

// Rate is the budget the newest response reported.
func (c *Conditional) Rate() RateLimit {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rate
}

// Replayed is how many 304 answers were replayed from the cache.
func (c *Conditional) Replayed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.replayed
}

func (c *Conditional) observe(h http.Header) {
	remaining, err := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if err != nil {
		return
	}
	reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.rate = RateLimit{Known: true, Remaining: remaining, Reset: time.Unix(reset, 0)}
	c.mu.Unlock()
}
