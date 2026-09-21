// Package registry mocks an OCI distribution registry the way a release's
// artifacts are probed: HEAD and GET of /v2/<name>/manifests/<reference> for
// images and for charts (names under charts/). The harness runs one instance
// as the public registry and one as the private registry, so a scenario can
// give them different states.
//
// Manifest routes have defaults a fixture rarely needs to spell out: a 200
// without a body is a minimal manifest with its Docker-Content-Digest, a 404
// without a body is the registry's MANIFEST_UNKNOWN error, and a manifest no
// route scripts is MANIFEST_UNKNOWN too. With the stale-login flag set every
// request is answered 401 UNAUTHORIZED with a bearer challenge, the answer a
// registry gives a client whose stored login has expired.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

// Media types of the default manifest.
const (
	ManifestMediaType    = "application/vnd.oci.image.manifest.v1+json"
	ImageConfigMediaType = "application/vnd.oci.image.config.v1+json"
	ChartConfigMediaType = "application/vnd.cncf.helm.config.v1+json"
)

var manifestPath = regexp.MustCompile(`^/v2/(.+)/manifests/([^/]+)$`)

// Server is a running mock.
type Server struct {
	*httptest.Server
	script     *sequence.Script
	staleLogin bool
}

// Start serves the routes on a loopback port until Close. With staleLogin
// every request is refused with 401 UNAUTHORIZED.
func Start(routes sequence.Routes, staleLogin bool) (*Server, error) {
	script, err := sequence.New(routes)
	if err != nil {
		return nil, err
	}
	s := &Server{script: script, staleLogin: staleLogin}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s, nil
}

// Host is host:port, the value of DEVCTL_REGISTRY_PUBLIC or
// DEVCTL_REGISTRY_PRIVATE; the registry speaks plain HTTP, hence
// DEVCTL_REGISTRY_INSECURE=1.
func (s *Server) Host() string {
	return strings.TrimPrefix(s.URL, "http://")
}

// Requests are the requests received so far.
func (s *Server) Requests() []sequence.Request {
	return s.script.Requests()
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	if s.staleLogin {
		s.script.Record(r)
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/oauth2/token",service="%s"`, s.URL, s.Host()))
		_ = sequence.Write(w, r, errorResponse(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil))
		return
	}
	resp, ok := s.script.Next(r)
	match := manifestPath.FindStringSubmatch(r.URL.Path)
	switch {
	case ok && match != nil:
		resp = withManifestDefaults(resp, match[1], match[2])
	case ok:
	case r.URL.Path == "/v2/":
		resp = sequence.Response{Body: map[string]any{}}
	case match != nil:
		resp = manifestUnknown(match[1], match[2])
	default:
		resp = errorResponse(http.StatusNotFound, "NAME_UNKNOWN", "repository name not known to registry", nil)
	}
	_ = sequence.Write(w, r, resp)
}

// withManifestDefaults fills in what a manifest route's fixture left out.
func withManifestDefaults(resp sequence.Response, name, reference string) sequence.Response {
	if resp.Body != nil {
		return resp
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		body := Manifest(name)
		headers := map[string]string{
			"Content-Type":          ManifestMediaType,
			"Docker-Content-Digest": Digest(body),
		}
		for k, v := range resp.Headers {
			headers[k] = v
		}
		return sequence.Response{Status: resp.Status, Headers: headers, Body: body}
	case http.StatusNotFound:
		unknown := manifestUnknown(name, reference)
		unknown.Headers = resp.Headers
		return unknown
	}
	return resp
}

// Manifest is the default manifest body of an available artifact: an OCI
// image manifest without layers, its config a chart's for a name under
// charts/.
func Manifest(name string) string {
	configMediaType := ImageConfigMediaType
	if strings.Contains(name, "/charts/") || strings.HasPrefix(name, "charts/") {
		configMediaType = ChartConfigMediaType
	}
	return fmt.Sprintf(`{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":%q,"digest":%q,"size":2},"layers":[]}`,
		ManifestMediaType, configMediaType, Digest("{}"))
}

// Digest is the sha256 digest of a body in the form a registry reports it.
func Digest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func manifestUnknown(name, reference string) sequence.Response {
	return errorResponse(http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest unknown",
		map[string]any{"Name": name, "Revision": reference})
}

func errorResponse(status int, code, message string, detail any) sequence.Response {
	return sequence.Response{
		Status: status,
		Body: map[string]any{
			"errors": []map[string]any{{"code": code, "message": message, "detail": detail}},
		},
	}
}
