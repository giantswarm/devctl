package releasewait

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// RegistryProber probes OCI registries with go-containerregistry: the public
// registry anonymously, so a stale docker login cannot produce a false
// UNAUTHORIZED for a public artifact; the private registry with the docker
// keychain, the credentials `docker login` stored.
type RegistryProber struct {
	// Endpoints name the registries and whether they speak plain HTTP.
	Endpoints agentcli.Endpoints
	// Keychain answers the private registry; nil means the docker keychain.
	Keychain authn.Keychain
	// Transport sends the requests; nil means the default.
	Transport http.RoundTripper
}

// The registry error codes that mean "not there yet": the manifest is
// unknown, or the repository does not exist because nothing was pushed to
// it yet (a first release).
var missingCodes = []transport.ErrorCode{transport.ManifestUnknownErrorCode, transport.NameUnknownErrorCode}

// Probe implements [Prober].
func (p RegistryProber) Probe(ctx context.Context, artifact Artifact) (string, bool, error) {
	var nameOpts []name.Option
	if p.Endpoints.RegistryInsecure {
		nameOpts = append(nameOpts, name.Insecure)
	}
	ref, err := name.ParseReference(artifact.Reference, nameOpts...)
	if err != nil {
		return "", false, fmt.Errorf("%q is not a registry reference: %w", artifact.Reference, err)
	}

	opts := []remote.Option{remote.WithContext(ctx)}
	if p.Transport != nil {
		opts = append(opts, remote.WithTransport(p.Transport))
	}
	if artifact.private {
		keychain := p.Keychain
		if keychain == nil {
			keychain = authn.DefaultKeychain
		}
		opts = append(opts, remote.WithAuthFromKeychain(keychain))
	} else {
		opts = append(opts, remote.WithAuth(authn.Anonymous))
	}

	descriptor, err := remote.Head(ref, opts...)
	if err == nil {
		return descriptor.Digest.String(), true, nil
	}
	var terr *transport.Error
	if !errors.As(err, &terr) {
		return "", false, err
	}
	for _, diagnostic := range terr.Errors {
		for _, code := range missingCodes {
			if diagnostic.Code == code {
				return "", false, nil
			}
		}
	}
	if terr.StatusCode == http.StatusNotFound && len(terr.Errors) == 0 {
		// A HEAD carries no body: a plain 404 is the manifest missing.
		return "", false, nil
	}
	answer := &RegistryAnswerError{Reference: artifact.Reference, Status: terr.StatusCode}
	if len(terr.Errors) > 0 {
		answer.Code = string(terr.Errors[0].Code)
	}
	switch {
	case terr.StatusCode == http.StatusUnauthorized && artifact.private:
		answer.Hint = fmt.Sprintf("the private registry refused the docker keychain's credentials; run `docker login %s`", ref.Context().RegistryStr())
	case terr.StatusCode == http.StatusUnauthorized:
		answer.Hint = "the public registry refused an anonymous read; the artifact is not public"
	case terr.StatusCode == http.StatusForbidden:
		answer.Hint = "the registry denied the read"
	}
	return "", false, answer
}
