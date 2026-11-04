package releasewait

import (
	"context"
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// namedImageArtifact accepts both leading-v spellings for an explicit
// --image. The git spelling is tried first; derived artifacts stay exact.
func namedImageArtifact(path, tag string, private bool, endpoints agentcli.Endpoints) Artifact {
	artifact := imageArtifact(path, tag, private, endpoints)
	if version, err := ParseVersion(tag); err == nil && version.Image == "" {
		artifact.alternateReference = imageArtifact(path, version.Tags()[1], private, endpoints).Reference
	}
	return artifact
}

// probeArtifact tries the alternate spelling only after a definitive
// missing answer. A refusal or transport error retains the normal failure
// handling; a digest records the pullable reference in the result.
func (w *Waiter) probeArtifact(ctx context.Context, artifact *Artifact) (string, bool, error) {
	digest, found, err := w.config.Registry.Probe(ctx, *artifact)
	if err != nil || found || artifact.alternateReference == "" {
		return digest, found, err
	}
	alternate := *artifact
	alternate.Reference = artifact.alternateReference
	digest, found, err = w.config.Registry.Probe(ctx, alternate)
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", alternate.Reference, err)
	}
	if found {
		artifact.Reference = alternate.Reference
	}
	return digest, found, nil
}
