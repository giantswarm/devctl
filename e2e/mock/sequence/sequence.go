// Package sequence scripts a mock's answers. A scenario names, per route, the
// sequence of responses the route gives: the Nth request to the route gets
// the Nth response and the last one repeats, so a poll loop sees state advance
// (pending, pending, completed) and a check that never reports stays where it
// is. The mocks of GitHub, CircleCI and the registry share this dispatcher and
// add their own headers and defaults on top.
package sequence

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Response is one scripted answer of a route.
type Response struct {
	// Status is the HTTP status; 0 reads as 200.
	Status int `yaml:"status"`
	// Headers are set on the response. A mock adds its own defaults for the
	// headers a fixture leaves out and never overrides one the fixture sets.
	Headers map[string]string `yaml:"headers"`
	// Body is sent as JSON when it is a mapping or a list, verbatim when it is
	// a string, and as an empty body when it is absent.
	Body any `yaml:"body"`
}

// StatusCode is the status the response is sent with.
func (r Response) StatusCode() int {
	if r.Status == 0 {
		return http.StatusOK
	}
	return r.Status
}

// Bytes is the body on the wire.
func (r Response) Bytes() ([]byte, error) {
	switch body := r.Body.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(body), nil
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("body is not JSON: %w", err)
		}
		return data, nil
	}
}

// Header is the fixture's headers as an http.Header, so lookups are
// case-insensitive.
func (r Response) Header() http.Header {
	h := http.Header{}
	for k, v := range r.Headers {
		h.Set(k, v)
	}
	return h
}

// Routes maps a route key, "METHOD /path" or "METHOD /path?key=value", to the
// route's responses. A key with a query matches a request whose query carries
// every listed parameter with the listed value, whatever else the request
// sends; a key without a query matches any query. Of several matching routes
// the one with the most query parameters wins.
type Routes map[string][]Response

// Request is one request a mock received.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
}

// String is "METHOD /path?query", the form of a route key.
func (r Request) String() string {
	if len(r.Query) == 0 {
		return r.Method + " " + r.Path
	}
	return r.Method + " " + r.Path + "?" + r.Query.Encode()
}

type route struct {
	key       string
	method    string
	path      string
	query     url.Values
	responses []Response
	calls     int
}

func (rt *route) matches(r *http.Request) bool {
	if rt.method != r.Method || rt.path != r.URL.Path {
		return false
	}
	query := r.URL.Query()
	for key, want := range rt.query {
		got, ok := query[key]
		if !ok || !slices.Equal(got, want) {
			return false
		}
	}
	return true
}

// Script dispatches requests to routes and records every request it sees.
type Script struct {
	mu       sync.Mutex
	routes   []*route
	requests []Request
}

// New compiles the routes. A key that is not "METHOD /path[?query]" or a
// route without responses is an error, so a scenario's typo fails its run.
func New(routes Routes) (*Script, error) {
	s := &Script{}
	for key, responses := range routes {
		method, target, ok := strings.Cut(key, " ")
		if !ok || method == "" || !strings.HasPrefix(target, "/") {
			return nil, fmt.Errorf("route %q: want \"METHOD /path[?query]\"", key)
		}
		u, err := url.ParseRequestURI(target)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", key, err)
		}
		if len(responses) == 0 {
			return nil, fmt.Errorf("route %q has no responses", key)
		}
		s.routes = append(s.routes, &route{
			key:       key,
			method:    strings.ToUpper(method),
			path:      u.Path,
			query:     u.Query(),
			responses: responses,
		})
	}
	// The most specific route first; the key order breaks ties so a run is
	// reproducible whatever the map iteration did.
	sort.Slice(s.routes, func(i, j int) bool {
		if len(s.routes[i].query) != len(s.routes[j].query) {
			return len(s.routes[i].query) > len(s.routes[j].query)
		}
		return s.routes[i].key < s.routes[j].key
	})
	return s, nil
}

// Next records the request and returns the matching route's next response.
// ok is false when no route matches; the request is recorded all the same.
func (s *Script) Next(r *http.Request) (resp Response, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record(r)
	for _, rt := range s.routes {
		if !rt.matches(r) {
			continue
		}
		resp = rt.responses[min(rt.calls, len(rt.responses)-1)]
		rt.calls++
		return resp, true
	}
	return Response{}, false
}

// Record notes a request the mock answers without consulting the script.
func (s *Script) Record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record(r)
}

func (s *Script) record(r *http.Request) {
	s.requests = append(s.requests, Request{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Header: r.Header.Clone(),
	})
}

// Requests are the requests received so far, in order.
func (s *Script) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Write sends the response: the fixture's headers, Content-Type
// application/json for a JSON body unless the fixture named one, the status,
// then the body (no body on a HEAD request, as the protocol demands).
func Write(w http.ResponseWriter, r *http.Request, resp Response) error {
	body, err := resp.Bytes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return err
	}
	h := w.Header()
	for key, values := range resp.Header() {
		h[key] = values
	}
	if _, isString := resp.Body.(string); !isString && len(body) > 0 && h.Get("Content-Type") == "" {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(resp.StatusCode())
	if r.Method == http.MethodHead || len(body) == 0 {
		return nil
	}
	_, err = w.Write(body)
	return err
}
