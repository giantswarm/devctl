package registry

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func TestManifestConnectionReset(t *testing.T) {
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		for _, status := range []int{0, http.StatusOK, http.StatusNotFound} {
			t.Run(fmt.Sprintf("%s/status=%d", method, status), func(t *testing.T) {
				const path = "/v2/giantswarm/devctl/manifests/v1.2.3"
				s, err := Start(sequence.Routes{
					method + " " + path: {
						{Reset: true, Status: status},
						{Status: http.StatusOK},
					},
				}, false)
				require.NoError(t, err)
				t.Cleanup(s.Close)

				// A fresh connection prevents net/http from replaying a request
				// after a reset and hiding the first scripted response.
				transport := &http.Transport{DisableKeepAlives: true}
				t.Cleanup(transport.CloseIdleConnections)
				client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
				req, err := http.NewRequest(method, s.URL+path, nil)
				require.NoError(t, err)
				resp, err := client.Do(req)
				if resp != nil {
					_ = resp.Body.Close()
				}
				require.ErrorContains(t, err, "connection reset by peer")

				req, err = http.NewRequest(method, s.URL+path, nil)
				require.NoError(t, err)
				resp, err = client.Do(req)
				require.NoError(t, err)
				defer func() { _ = resp.Body.Close() }()
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.Equal(t, Digest(Manifest("giantswarm/devctl")), resp.Header.Get("Docker-Content-Digest"))
				assert.Len(t, s.Requests(), 2, "the reset advances the route exactly once")
			})
		}
	}
}
