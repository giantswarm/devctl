package reservation_test

import (
	"fmt"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestReserveRefusesOverriddenDevSelectors(t *testing.T) {
	for _, tc := range []struct{ name, field, value string }{
		{"case 0: stable semver pin", "semver", "1.2.3"},
		{"case 1: tag override", "tag", "stable"},
		{"case 2: digest override", "digest", "sha256:0123456789abcdef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{collectionsKustomization: fmt.Sprintf(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../../bases/collections/demo
patches:
  - target:
      kind: OCIRepository
      namespace: giantswarm
    patch: |-
      - op: add
        path: /spec/ref/%s
        value: %s
`, tc.field, tc.value)})
			before := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
			_, err := reservation.Reserve(testRequest(dir))
			if !reservation.IsRenderAssertion(err) {
				t.Fatalf("want render assertion, got %v", err)
			}
			if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != before {
				t.Fatal("refused render moved HEAD")
			}
			if got := gittest.GitOutput(t, dir, "status", "--porcelain"); got != "" {
				t.Fatalf("refused render left writes: %s", got)
			}
		})
	}
}
