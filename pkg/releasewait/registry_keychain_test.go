package releasewait

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	registrymock "github.com/giantswarm/devctl/v8/e2e/mock/registry"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type recordingRegistryKeychain struct {
	resources []string
}

func (k *recordingRegistryKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	k.resources = append(k.resources, resource.RegistryStr())
	return authn.FromConfig(authn.AuthConfig{Username: "fixture-user", Password: "fixture-password"}), nil
}

func TestRegistryProberKeepsPublicReadsAnonymous(t *testing.T) {
	const path = "/v2/giantswarm/devctl/manifests/v1.2.3"
	for _, fallback := range []bool{false, true} {
		name := "HEAD"
		if fallback {
			name = "GET fallback"
		}
		t.Run(name, func(t *testing.T) {
			head := sequence.Response{Status: http.StatusOK}
			if fallback {
				head.Headers = map[string]string{"Docker-Content-Digest": ""}
			}
			s, err := registrymock.Start(sequence.Routes{
				"HEAD " + path: {head},
				"GET " + path:  {{Status: http.StatusOK}},
			}, true)
			require.NoError(t, err)
			t.Cleanup(s.Close)
			keychain := &recordingRegistryKeychain{}
			transport := &http.Transport{}
			t.Cleanup(transport.CloseIdleConnections)
			prober := RegistryProber{
				Endpoints: agentcli.Endpoints{RegistryInsecure: true},
				Keychain:  keychain,
				Transport: transport,
			}

			digest, found, err := prober.Probe(t.Context(), Artifact{Reference: s.Host() + "/giantswarm/devctl:v1.2.3"})
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, registrymock.Digest(registrymock.Manifest("giantswarm/devctl")), digest)
			assert.Empty(t, keychain.resources, "public reads must never resolve the supplied private keychain")
			var methods []string
			for _, request := range s.Requests() {
				assert.Empty(t, request.Header.Get("Authorization"))
				if request.Path == path {
					methods = append(methods, request.Method)
				}
			}
			wantMethods := []string{http.MethodHead}
			if fallback {
				wantMethods = append(wantMethods, http.MethodGet)
			}
			assert.Equal(t, wantMethods, methods)
		})
	}
}

func TestRegistryProberUsesPrivateKeychain(t *testing.T) {
	const path = "/v2/giantswarm/devctl/manifests/v1.2.3"
	body := registrymock.Manifest("giantswarm/devctl")
	var authenticatedManifest atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "fixture-user" || password != "fixture-password" {
			w.Header().Set("WWW-Authenticate", `Basic realm="fixture-registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != path || r.Method != http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		authenticatedManifest.Store(true)
		w.Header().Set("Content-Type", registrymock.ManifestMediaType)
		w.Header().Set("Docker-Content-Digest", registrymock.Digest(body))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.Close)
	host := strings.TrimPrefix(s.URL, "http://")
	keychain := &recordingRegistryKeychain{}
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	prober := RegistryProber{
		Endpoints: agentcli.Endpoints{RegistryInsecure: true},
		Keychain:  keychain,
		Transport: transport,
	}

	digest, found, err := prober.Probe(t.Context(), Artifact{Reference: host + "/giantswarm/devctl:v1.2.3", private: true})
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, registrymock.Digest(body), digest)
	assert.True(t, authenticatedManifest.Load(), "the private manifest requires the synthetic keychain credentials")
	assert.Equal(t, []string{host}, keychain.resources)
}
