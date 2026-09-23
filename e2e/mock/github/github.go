// Package github mocks the GitHub REST API and the device-flow endpoints of
// github.com on one server: DEVCTL_GITHUB_API_URL and DEVCTL_GITHUB_OAUTH_URL
// both point at it. Every response comes from the scenario's route sequences;
// the mock adds what the real API adds and a client relies on: the rate-limit
// headers the poll interval is read from, an ETag on every successful body,
// and 304 Not Modified when a conditional request carries that ETag.
package github

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

// NotFound is what an unscripted route answers, the way api.github.com does.
var NotFound = sequence.Response{
	Status: http.StatusNotFound,
	Body: map[string]any{
		"message":           "Not Found",
		"documentation_url": "https://docs.github.com/rest",
		"status":            "404",
	},
}

// Server is a running mock.
type Server struct {
	*httptest.Server
	script *sequence.Script
}

// Start serves the routes on a loopback port until Close.
func Start(routes sequence.Routes) (*Server, error) {
	script, err := sequence.New(routes)
	if err != nil {
		return nil, err
	}
	s := &Server{script: script}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s, nil
}

// Requests are the requests received so far.
func (s *Server) Requests() []sequence.Request {
	return s.script.Requests()
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	resp, ok := s.script.Next(r)
	if !ok {
		resp = NotFound
	}
	if resp.Reset {
		_ = sequence.Write(w, r, resp)
		return
	}
	resp, err := relativeReset(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fixture := resp.Header()
	h := w.Header()
	for key, value := range rateLimitDefaults() {
		if fixture.Get(key) == "" {
			h.Set(key, value)
		}
	}
	if resp.StatusCode() < http.StatusMultipleChoices {
		etag := fixture.Get("ETag")
		if etag == "" {
			body, err := resp.Bytes()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			etag = ETag(body)
		}
		h.Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	_ = sequence.Write(w, r, resp)
}

// ETag is the tag the mock gives a body: a fixture that wants to assert on it
// computes the same.
func ETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// relativeReset turns an X-RateLimit-Reset of the form "+3s" into the Unix
// time that far from now, so a fixture scripts a budget that resets a few
// seconds into the run.
func relativeReset(resp sequence.Response) (sequence.Response, error) {
	for key, value := range resp.Headers {
		if !strings.EqualFold(key, "X-RateLimit-Reset") || !strings.HasPrefix(value, "+") {
			continue
		}
		d, err := time.ParseDuration(value[1:])
		if err != nil {
			return resp, fmt.Errorf("%s %q: %w", key, value, err)
		}
		headers := maps.Clone(resp.Headers)
		headers[key] = strconv.FormatInt(time.Now().Add(d).Unix(), 10)
		resp.Headers = headers
	}
	return resp, nil
}

// rateLimitDefaults are the headers of a request well inside the budget; a
// fixture that wants the client to slow down sets its own.
func rateLimitDefaults() map[string]string {
	return map[string]string{
		"X-RateLimit-Limit":     "5000",
		"X-RateLimit-Remaining": "4999",
		"X-RateLimit-Used":      "1",
		"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10),
		"X-RateLimit-Resource":  "core",
	}
}
