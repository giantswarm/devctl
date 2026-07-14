package githubclient

import (
	"bytes"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"
)

// dryRunTransport short-circuits non-safe HTTP methods with a synthetic
// 200 OK and an empty JSON body. Safe methods pass through unchanged.
type dryRunTransport struct {
	inner  http.RoundTripper
	logger *logrus.Logger
}

func (t *dryRunTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if isSafeHTTPMethod(req.Method) {
		return t.inner.RoundTrip(req)
	}

	if req.Body != nil {
		_ = req.Body.Close()
	}

	t.logger.Debugf("[dry-run] skipped %s %s", req.Method, req.URL.Path)

	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Proto:      req.Proto,
		ProtoMajor: req.ProtoMajor,
		ProtoMinor: req.ProtoMinor,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
		Request:    req,
	}, nil
}

func isSafeHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
