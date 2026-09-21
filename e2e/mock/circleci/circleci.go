// Package circleci mocks the CircleCI API v2 and CircleCI's OAuth issuer on
// one server: DEVCTL_CIRCLECI_API_URL is the server's URL plus APIPrefix, the
// path circleci.com serves the API under, and DEVCTL_CIRCLECI_OAUTH_URL is the
// server's URL. Every response comes from the scenario's route sequences,
// keyed by the full path ("GET /api/v2/project/gh/owner/repo/pipeline").
package circleci

import (
	"net/http"
	"net/http/httptest"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

// APIPrefix is the path the API v2 lives under.
const APIPrefix = "/api/v2"

// NotFound is what an unscripted route answers, the way circleci.com does.
var NotFound = sequence.Response{
	Status: http.StatusNotFound,
	Body:   map[string]any{"message": "Not Found"},
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

// APIURL is the value of DEVCTL_CIRCLECI_API_URL.
func (s *Server) APIURL() string {
	return s.URL + APIPrefix
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
	_ = sequence.Write(w, r, resp)
}
