package release

import (
	"testing"

	"github.com/giantswarm/releases/sdk/api/v1alpha1"
)

func TestImageBuilderVersionRegexp(t *testing.T) {
	testCases := []struct {
		name       string
		dockerfile string
		expected   string
	}{
		{
			name: "case 0: pin as written in capi-image-builder",
			dockerfile: `# repo: kubernetes-sigs/image-builder
ARG IMAGE_BUILDER_VERSION=v0.1.55

FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.26-alpine as builder
`,
			expected: "v0.1.55",
		},
		{
			name:       "case 1: no v prefix",
			dockerfile: "ARG IMAGE_BUILDER_VERSION=0.1.48\n",
			expected:   "0.1.48",
		},
		{
			name:       "case 2: the later reference does not match",
			dockerfile: "FROM registry.k8s.io/scl-image-builder/cluster-node-image-builder-amd64:${IMAGE_BUILDER_VERSION}\n",
			expected:   "",
		},
		{
			name:       "case 3: no pin at all",
			dockerfile: "FROM alpine:3.20\n",
			expected:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			match := imageBuilderVersionRegexp.FindStringSubmatch(tc.dockerfile)

			actual := ""
			if match != nil {
				actual = match[1]
			}

			if actual != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestLookupComponentVersion(t *testing.T) {
	components := []v1alpha1.ReleaseSpecComponent{
		{Name: "cluster-aws", Version: "7.7.3"},
		{Name: "os-tooling", Version: "1.33.1"},
	}

	if actual := lookupComponentVersion(components, "os-tooling"); actual != "1.33.1" {
		t.Fatalf("expected 1.33.1, got %q", actual)
	}
	if actual := lookupComponentVersion(components, "containerd"); actual != "" {
		t.Fatalf("expected empty version for a missing component, got %q", actual)
	}
}

// A release without os-tooling has no node image we build, so there is nothing to derive and
// applyContainerdComponent must not reach out to GitHub. That makes this safe to assert offline.
func TestApplyContainerdComponentWithoutOSTooling(t *testing.T) {
	// AKS uses provider-managed node images and has no os-tooling component.
	updates := v1alpha1.Release{
		Spec: v1alpha1.ReleaseSpec{
			Components: []v1alpha1.ReleaseSpecComponent{{Name: "kubernetes", Version: "1.35.7"}},
		},
	}
	base := v1alpha1.Release{
		Spec: v1alpha1.ReleaseSpec{
			Components: []v1alpha1.ReleaseSpecComponent{{Name: "cluster-aks", Version: "0.5.0"}},
		},
	}

	applyContainerdComponent(&updates, base, false)

	if version := lookupComponentVersion(updates.Spec.Components, containerdComponentName); version != "" {
		t.Fatalf("expected no containerd component, got %q", version)
	}
	if len(updates.Spec.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(updates.Spec.Components))
	}
}
