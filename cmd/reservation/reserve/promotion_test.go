package reserve_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/devctl/v8/cmd/reservation/reserve"
	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// A self-contained collection keeps the public command fixture offline.
var gitopsFiles = map[string]string{
	"bases/collections/demo/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - hello-world.yaml
`,
	"bases/collections/demo/hello-world.yaml": `---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: hello-world
  namespace: giantswarm
spec:
  interval: 10m
  provider: generic
  ref:
    semver: x.x.x
  url: oci://gsoci.azurecr.io/charts/giantswarm/hello-world
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
  targetNamespace: giantswarm
`,
	"management-clusters/graveler/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - flux-collections.yaml
  - configmap-reservations.yaml
`,
	"management-clusters/graveler/flux-collections.yaml": `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: collections
  namespace: flux-giantswarm
spec:
  interval: 10m
  path: management-clusters/graveler/collections
  prune: true
  sourceRef:
    kind: GitRepository
    name: management-clusters-fleet
`,
	"management-clusters/graveler/collections/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../../bases/collections/demo
`,
	"management-clusters/graveler/configmap-reservations.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: reservations
  namespace: giantswarm
  annotations:
    reservations.giantswarm.io/max-duration: 7d
data: {}
`,
}

func newReserveCheckout(t *testing.T) string {
	t.Helper()
	origin, dir := t.TempDir(), t.TempDir()
	gittest.RunGit(t, origin, "init", "--bare", "-b", "main")
	gittest.RunGit(t, dir, "init", "-b", "main")
	gittest.RunGit(t, dir, "remote", "add", "origin", origin)
	for name, content := range gitopsFiles {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gittest.RunGit(t, dir, "add", "-A")
	gittest.RunGit(t, dir, "commit", "-m", "fixture")
	gittest.RunGit(t, dir, "push", "-u", "origin", "main")
	return dir
}

func runReserve(t *testing.T, dir, output string, extra ...string) (string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := reserve.New(reserve.Config{Logger: logger, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetArgs(append([]string{
		"--gitops-dir", dir, "--cluster", "graveler", "--app", "hello-world",
		"--branch", "fix/crash", "--user", "alice", "--output", output,
	}, extra...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("reserve: %v; stderr: %s", err, stderr.String())
	}
	return stdout.String(), stderr.String()
}

func storedReservation(t *testing.T, dir string) reservation.Reservation {
	t.Helper()
	entries, err := reservation.List(reservation.ListRequest{RepoDir: dir, Cluster: "graveler"})
	if err != nil || len(entries) != 1 {
		t.Fatalf("stored reservations = %v, err = %v", entries, err)
	}
	return entries[0]
}

func TestReserveReportsEffectivePullRequest(t *testing.T) {
	for _, output := range []string{"json", "table"} {
		for _, pr := range []string{"", "giantswarm/hello-world#123"} {
			t.Run(output+"/"+pr, func(t *testing.T) {
				dir := newReserveCheckout(t)
				var args []string
				if pr != "" {
					args = []string{"--pull-request", pr}
				}
				_, stderr := runReserve(t, dir, output, args...)
				if got := strings.Contains(stderr, "do not find this reservation"); got != (pr == "") {
					t.Errorf("initial lifecycle warning = %v; stderr: %s", got, stderr)
				}
				before := storedReservation(t, dir)
				stdout, stderr := runReserve(t, dir, output, "--exclusive", "--duration", "2h")
				if got := strings.Contains(stderr, "do not find this reservation"); got != (pr == "") {
					t.Errorf("promotion lifecycle warning = %v; stderr: %s", got, stderr)
				}
				after := storedReservation(t, dir)
				want := before
				want.Scope = reservation.ScopeExclusive
				if after != want {
					t.Errorf("promotion stored %+v, want %+v", after, want)
				}
				if output == "json" {
					var doc reservation.Reservation
					if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
						t.Fatal(err)
					}
					if doc != after {
						t.Errorf("JSON reports %+v, stored %+v", doc, after)
					}
				}
			})
		}
	}
}
