package ami

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/giantswarm/microerror"
)

type integrityTransport func(*http.Request) (*http.Response, error)

func (f integrityTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type failedListingReader struct{}

func (failedListingReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestReleaseFetchRejectsHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		listing bool
		status  int
	}{
		{name: "listing forbidden", listing: true, status: http.StatusForbidden},
		{name: "listing server error", listing: true, status: http.StatusServiceUnavailable},
		{name: "release server error", status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = integrityTransport(func(r *http.Request) (*http.Response, error) {
				status := tc.status
				body := "<html>temporarily unavailable</html>"
				if !tc.listing {
					if strings.HasSuffix(r.URL.Path, ".json") {
						body = "temporarily unavailable"
					} else {
						status = http.StatusOK
						body = `<a href="1.2.3/">release</a>`
					}
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})

			got, err := getAMIInfoString(Config{Channel: "stable", PrimaryDomain: "fixture.invalid", Arch: "amd64-usr", MinimumVersion: "1.0.0"})
			if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.status)) {
				t.Fatalf("got output %q, error %v; want HTTP status error", got, err)
			}
			if got != "" {
				t.Fatalf("returned publishable output %q after HTTP failure", got)
			}
		})
	}
}

func TestReleaseListingReadFailureIsReturned(t *testing.T) {
	for _, prefix := range []string{"", `<a href="1.2.3/">release</a>`} {
		t.Run(prefix, func(t *testing.T) {
			existingPath := filepath.Join(t.TempDir(), "existing.json")
			if err := os.WriteFile(existingPath, []byte(`{"1.2.3":{"eu-west-1":"ami-fixture"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = integrityTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, ".json") {
					return nil, errors.New("unexpected release request")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(io.MultiReader(strings.NewReader(prefix), failedListingReader{})), Header: make(http.Header)}, nil
			})

			got, err := getAMIInfoString(Config{Channel: "stable", PrimaryDomain: "fixture.invalid", Arch: "amd64-usr", MinimumVersion: "1.0.0", KeepExisting: existingPath})
			if microerror.Cause(err) != io.ErrUnexpectedEOF || got != "" {
				t.Fatalf("got output %q, error %v; want no output and unexpected EOF", got, err)
			}
		})
	}
}

func TestReleaseFetchAllowsEmptyListingAndMissingAMI(t *testing.T) {
	for _, listing := range []string{"", `<a href="1.2.3/">release</a>`} {
		t.Run(listing, func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = integrityTransport(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, listing
				if strings.HasSuffix(r.URL.Path, ".json") {
					status, body = http.StatusForbidden, "not published"
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			got, err := getAMIInfoString(Config{Channel: "stable", PrimaryDomain: "fixture.invalid", Arch: "amd64-usr", MinimumVersion: "1.0.0"})
			if err != nil || got != "{}" {
				t.Fatalf("got %q, %v; want {}, nil", got, err)
			}
		})
	}
}
