package ami

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type ownershipTransport func(*http.Request) (*http.Response, error)

func (f ownershipTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type ownedHTTPBody struct {
	io.Reader
	closed bool
}

func (b *ownedHTTPBody) Close() error {
	b.closed = true
	return nil
}

type ownershipReadError struct{}

func (ownershipReadError) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestReleaseHTTPBodiesAreClosed(t *testing.T) {
	for _, tc := range []struct {
		name          string
		listing       io.Reader
		listingStatus int
		releaseStatus int
		releaseBody   string
	}{
		{name: "empty listing", listing: strings.NewReader("")},
		{name: "missing releases", listing: strings.NewReader(`<a href="1.2.3/">first</a><a href="1.2.4/">second</a>`), releaseStatus: http.StatusForbidden, releaseBody: "unread error page"},
		{name: "invalid release JSON", listing: strings.NewReader(`<a href="1.2.3/">release</a>`), releaseStatus: http.StatusOK, releaseBody: "invalid JSON"},
		{name: "listing read error", listing: ownershipReadError{}},
		{name: "listing server error", listing: strings.NewReader("unread error page"), listingStatus: http.StatusServiceUnavailable},
		{name: "release server error", listing: strings.NewReader(`<a href="1.2.3/">release</a>`), releaseStatus: http.StatusServiceUnavailable, releaseBody: "unread error page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var bodies []*ownedHTTPBody
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = ownershipTransport(func(r *http.Request) (*http.Response, error) {
				for i, body := range bodies {
					if !body.closed {
						t.Errorf("response body %d still open when starting next request", i)
					}
				}
				body := &ownedHTTPBody{Reader: tc.listing}
				status := http.StatusOK
				if tc.listingStatus != 0 {
					status = tc.listingStatus
				}
				if strings.HasSuffix(r.URL.Path, ".json") {
					status, body.Reader = tc.releaseStatus, strings.NewReader(tc.releaseBody)
				}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: status, Body: body, Header: make(http.Header)}, nil
			})

			_, _ = getAMIInfoString(context.Background(), Config{Channel: "stable", PrimaryDomain: "fixture.invalid", Arch: "amd64-usr", MinimumVersion: "1.0.0"})
			for i, body := range bodies {
				if !body.closed {
					t.Errorf("response body %d still open after scrape returns", i)
				}
			}
		})
	}
}
