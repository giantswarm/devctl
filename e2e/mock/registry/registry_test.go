package registry

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func do(t *testing.T, method, url string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body)
}

func TestManifestBecomesAvailable(t *testing.T) {
	s, err := Start(sequence.Routes{
		"HEAD /v2/giantswarm/devctl/manifests/v1.2.3": {
			{Status: http.StatusNotFound},
			{Status: http.StatusOK},
		},
	}, false)
	require.NoError(t, err)
	defer s.Close()

	url := "http://" + s.Host() + "/v2/giantswarm/devctl/manifests/v1.2.3"
	resp, _ := do(t, http.MethodHead, url)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "registry/2.0", resp.Header.Get("Docker-Distribution-API-Version"))

	resp, body := do(t, http.MethodHead, url)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, body, "HEAD carries no body")
	assert.Equal(t, ManifestMediaType, resp.Header.Get("Content-Type"))
	assert.Equal(t, Digest(Manifest("giantswarm/devctl")), resp.Header.Get("Docker-Content-Digest"))
	assert.NotEmpty(t, resp.Header.Get("Content-Length"))

	resp, _ = do(t, http.MethodHead, url)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the last response repeats")
	assert.Len(t, s.Requests(), 3)
}

func TestManifestGetAndChartConfig(t *testing.T) {
	s, err := Start(sequence.Routes{
		"GET /v2/giantswarm/charts/devctl/manifests/1.2.3": {
			{Status: http.StatusOK, Headers: map[string]string{"Docker-Content-Digest": "sha256:pinned"}},
		},
	}, false)
	require.NoError(t, err)
	defer s.Close()

	resp, body := do(t, http.MethodGet, s.URL+"/v2/giantswarm/charts/devctl/manifests/1.2.3")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "sha256:pinned", resp.Header.Get("Docker-Content-Digest"), "the fixture's digest wins")
	assert.Contains(t, body, ChartConfigMediaType)
	assert.Contains(t, body, `"schemaVersion":2`)
}

func TestUnscriptedManifestIsUnknown(t *testing.T) {
	s, err := Start(sequence.Routes{}, false)
	require.NoError(t, err)
	defer s.Close()

	resp, body := do(t, http.MethodGet, s.URL+"/v2/giantswarm/other/manifests/v9.9.9")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.JSONEq(t, `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown","detail":{"Name":"giantswarm/other","Revision":"v9.9.9"}}]}`, body)

	resp, body = do(t, http.MethodGet, s.URL+"/v2/")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the API version check passes")
	assert.JSONEq(t, `{}`, body)

	resp, body = do(t, http.MethodGet, s.URL+"/v2/giantswarm/other/tags/list")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Contains(t, body, "NAME_UNKNOWN")
}

func TestScriptedNotFoundGetsTheErrorBody(t *testing.T) {
	s, err := Start(sequence.Routes{
		"GET /v2/giantswarm/devctl/manifests/v1.2.3": {{Status: http.StatusNotFound}},
	}, false)
	require.NoError(t, err)
	defer s.Close()

	_, body := do(t, http.MethodGet, s.URL+"/v2/giantswarm/devctl/manifests/v1.2.3")
	assert.Contains(t, body, "MANIFEST_UNKNOWN")
}

func TestStaleLoginRefusesEverything(t *testing.T) {
	s, err := Start(sequence.Routes{
		"HEAD /v2/giantswarm/devctl/manifests/v1.2.3": {{Status: http.StatusOK}},
	}, true)
	require.NoError(t, err)
	defer s.Close()

	for _, target := range []string{"/v2/", "/v2/giantswarm/devctl/manifests/v1.2.3"} {
		resp, body := do(t, http.MethodGet, s.URL+target)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, target)
		assert.Contains(t, resp.Header.Get("WWW-Authenticate"), `Bearer realm="`+s.URL+`/oauth2/token"`, target)
		assert.Contains(t, body, "UNAUTHORIZED", target)
	}
	assert.Len(t, s.Requests(), 2, "refused requests are recorded")
}
