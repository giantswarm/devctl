package reservation_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// The fixture is a git working tree shaped like a `<customer>-management-clusters`
// repo with one management cluster. The collection base sits in the same tree
// under bases/, standing in for the remote `management-cluster-bases` URL that a
// real cluster references, so a render needs no network.
const (
	fixtureCluster = "graveler"
	fixtureApp     = "hello-world"
)

type fixtureOptions struct {
	// omitConfigMap leaves out configmap-reservations.yaml, so the cluster is
	// not opted in to reservations.
	omitConfigMap bool
	// collectionsKustomization overrides the cluster's collections/kustomization.yaml.
	collectionsKustomization string
	// clusters names the management clusters to write. Empty means one cluster,
	// fixtureCluster.
	clusters []string
}

// newGitOpsFixture writes the fixture into a temporary directory, commits it and
// returns the working tree root.
func newGitOpsFixture(t *testing.T, opts fixtureOptions) string {
	t.Helper()

	dir := t.TempDir()

	files := map[string]string{
		// --- the MCB-shaped collection base --------------------------------
		"bases/collections/demo/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - hello-world.yaml
  - other-app.yaml
components:
  # Last, and targeting OCIRepository by kind with no name, exactly as the real
  # testing stage does.
  - ../shared/semver-rc-or-stable
`,
		"bases/collections/demo/hello-world.yaml": `---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: hello-world
  namespace: giantswarm
spec:
  insecure: false
  interval: 10m
  layerSelector:
    mediaType: application/vnd.cncf.helm.chart.content.v1.tar+gzip
  provider: generic
  ref:
    semver: x.x.x
  secretRef:
    name: container-registries-configuration
  url: oci://gsoci.azurecr.io/charts/giantswarm/hello-world
  verify:
    provider: cosign
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: hello-world
  namespace: giantswarm
spec:
  chartRef:
    kind: OCIRepository
    name: hello-world
    namespace: giantswarm
  interval: 5m
  releaseName: hello-world
  storageNamespace: giantswarm
  targetNamespace: giantswarm
`,
		"bases/collections/demo/other-app.yaml": `---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: other-app
  namespace: giantswarm
spec:
  interval: 10m
  provider: generic
  ref:
    semver: x.x.x
  url: oci://gsoci.azurecr.io/charts/giantswarm/other-app
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: other-app
  namespace: giantswarm
spec:
  chartRef:
    kind: OCIRepository
    name: other-app
    namespace: giantswarm
  interval: 5m
  releaseName: other-app
  storageNamespace: giantswarm
  targetNamespace: giantswarm
`,
		"bases/collections/shared/semver-rc-or-stable/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1alpha1
kind: Component
patches:
  - target:
      kind: OCIRepository
      namespace: giantswarm
    path: semver-rc-or-stable-patch.yaml
`,
		"bases/collections/shared/semver-rc-or-stable/semver-rc-or-stable-patch.yaml": `- op: replace
  path: /spec/ref/semver
  value: ">=0.0.0-0"
- op: add
  path: /spec/ref/semverFilter
  value: '^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$'
`,
	}

	if opts.collectionsKustomization == "" {
		opts.collectionsKustomization = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  # Stands in for the remote management-cluster-bases collection stage.
  - ../../../bases/collections/demo
`
	}
	if len(opts.clusters) == 0 {
		opts.clusters = []string{fixtureCluster}
	}

	for _, cluster := range opts.clusters {
		root := "management-clusters/" + cluster + "/"

		files[root+"kustomization.yaml"] = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - collections
  - configmap-reservations.yaml
`
		files[root+"collections/kustomization.yaml"] = opts.collectionsKustomization

		if !opts.omitConfigMap {
			files[root+"configmap-reservations.yaml"] = `apiVersion: v1
kind: ConfigMap
metadata:
  name: reservations
  namespace: giantswarm
  annotations:
    reservations.giantswarm.io/max-duration: 7d
    reservations.giantswarm.io/slack-channel: "#reservations"
data: {}
`
		}
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // a test fixture
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // a test fixture
			t.Fatal(err)
		}
	}

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.AddGlob("."); err != nil {
		t.Fatal(err)
	}
	_, err = wt.Commit("fixture", &git.CommitOptions{
		Author: &object.Signature{Name: "fixture", Email: "fixture@example.com", When: time.Unix(0, 0).UTC()},
	})
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

// readFixtureFile returns the content of a repo-relative path, failing the test
// when it does not exist.
func readFixtureFile(t *testing.T, dir, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a test fixture
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	return string(b)
}
