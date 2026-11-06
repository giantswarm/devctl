package registry

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func TestStaleLoginPreservesManifestSequence(t *testing.T) {
	const path = "/v2/giantswarm/devctl/manifests/v1.2.3"
	s, err := Start(sequence.Routes{
		"HEAD " + path: {
			{Status: http.StatusNotFound},
			{Status: http.StatusOK},
		},
	}, true)
	require.NoError(t, err)
	t.Cleanup(s.Close)

	req, err := http.NewRequest(http.MethodHead, s.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer expired-fixture")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), `Bearer realm="`+s.URL+`/oauth2/token"`)

	resp, _ = do(t, http.MethodHead, s.URL+path)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "rejection must not consume the first matching response")
	resp, _ = do(t, http.MethodHead, s.URL+path)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, Digest(Manifest("giantswarm/devctl")), resp.Header.Get("Docker-Content-Digest"))

	requests := s.Requests()
	require.Len(t, requests, 3)
	for i, request := range requests {
		assert.Equal(t, http.MethodHead, request.Method)
		assert.Equal(t, path, request.Path)
		if i == 0 {
			assert.Equal(t, "Bearer expired-fixture", request.Header.Get("Authorization"))
		} else {
			assert.Empty(t, request.Header.Get("Authorization"))
		}
	}
}
