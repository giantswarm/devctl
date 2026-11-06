package registry

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func TestManifestFixtureHeadersIgnoreCase(t *testing.T) {
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		for _, tc := range []struct {
			name        string
			contentType string
			digest      string
		}{
			{name: "overrides", contentType: "application/vnd.docker.distribution.manifest.v2+json", digest: "sha256:pinned"},
			{name: "empty", contentType: "", digest: ""},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				const path = "/v2/giantswarm/devctl/manifests/v1.2.3"
				headers := map[string]string{
					"content-type":          tc.contentType,
					"docker-content-digest": tc.digest,
				}
				defaults := withManifestDefaults(sequence.Response{Headers: headers}, "giantswarm/devctl", "v1.2.3")
				assert.Len(t, defaults.Headers, 2, "defaults must not introduce case variants of fixture headers")
				s, err := Start(sequence.Routes{
					method + " " + path: {{Headers: headers}},
				}, false)
				require.NoError(t, err)
				t.Cleanup(s.Close)

				resp, body := do(t, method, s.URL+path)
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.Equal(t, tc.contentType, resp.Header.Get("Content-Type"))
				assert.Equal(t, tc.digest, resp.Header.Get("Docker-Content-Digest"))
				if method == http.MethodGet {
					assert.Equal(t, Manifest("giantswarm/devctl"), body)
				} else {
					assert.Empty(t, body)
				}
				assert.Equal(t, map[string]string{
					"content-type":          tc.contentType,
					"docker-content-digest": tc.digest,
				}, headers, "defaults leave the fixture unchanged")
			})
		}
	}
}
