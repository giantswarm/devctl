package checkextras

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const block = `patches:
  # BEGIN reservation of app, written by ` + "`devctl reservation reserve`" + `. Do not edit.
  - target:
      kind: OCIRepository
      name: app
    patch: |-
      - op: add
        path: /spec/ref
        value: {semver: ">=0.0.0-0"}
  # END reservation of app, written by ` + "`devctl reservation reserve`" + `
`

const humanPatch = `  - target:
      kind: OCIRepository
      name: app
    patch: |-
      - op: replace
        path: /spec/ref/semver
        value: ">=2.0.0"
`

func TestCheckExtras(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		ok      bool
	}{
		// The collections kustomization holds blocks the same way.
		"collections: patch after a block": {"resources:\n  - ../bases/demo\n" + block + humanPatch, false},
		"no patches":                       {"resources:\n  - ./app/\n", true},
		"block last":                       {"resources:\n  - ./app/\n" + block, true},
		"patch after a block":              {"resources:\n  - ./app/\n" + block + humanPatch, false},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kustomization.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			cmd, err := New(Config{Stdout: &stdout})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetArgs([]string{path})
			cmd.SilenceUsage, cmd.SilenceErrors = true, true

			err = cmd.Execute()
			if tc.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), path)) {
				t.Errorf("expected an error naming %s, got %v", path, err)
			}
		})
	}
}

// TestCheckExtrasReportsEveryFile: CI passes the extras of every touched
// cluster, and one run should name every bad file, not only the first.
func TestCheckExtrasReportsEveryFile(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, f := range []struct{ name, content string }{
		{"bad1.yaml", "resources:\n  - ./app/\n" + block + humanPatch},
		{"good.yaml", "resources:\n  - ./app/\n" + block},
		{"bad2.yaml", "resources:\n  - ./app/\n" + block + humanPatch},
	} {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte(f.content), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	var stdout bytes.Buffer
	cmd, err := New(Config{Stdout: &stdout})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetArgs(paths)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true

	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), paths[0]) || !strings.Contains(err.Error(), paths[2]) {
		t.Errorf("expected an error naming %s and %s, got %v", paths[0], paths[2], err)
	}
	if want := paths[1] + ": ok\n"; stdout.String() != want {
		t.Errorf("stdout: got %q, want %q", stdout.String(), want)
	}
}

// bundleBlock is the block of a bundle member as reserve writes it. create
// picks the form: the patch that creates spec.postRenderers, or the one that
// appends to it.
func bundleBlock(create bool) string {
	path, value := "/spec/postRenderers/-", "\n          kustomize:"
	if create {
		path, value = "/spec/postRenderers", "\n          - kustomize:"
	}

	return `patches:
  # BEGIN reservation of alloy, written by ` + "`devctl reservation reserve`" + `. Do not edit.
  - target:
      kind: HelmRelease
      name: bundle
      namespace: giantswarm
    patch: |-
      - op: add
        path: ` + path + `
        value:` + value + `
              patches:
                - target:
                    kind: OCIRepository
                    labelSelector: bundle.giantswarm.io/member-chart=alloy
                  patch: |-
                    - op: add
                      path: /spec/interval
                      value: 1m
  # END reservation of alloy, written by ` + "`devctl reservation reserve`" + `
`
}

const bundleRelease = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: bundle
  namespace: giantswarm
spec:
  chartRef:
    kind: OCIRepository
    name: bundle
    namespace: giantswarm
  interval: 5m
`

const ownPostRenderer = `  postRenderers:
    - kustomize:
        patches:
          - target:
              kind: Deployment
            patch: |-
              - op: add
                path: /metadata/labels/team
                value: honeybadger
`

// laterPostRenderer appends a post-renderer after the blocks.
const laterPostRenderer = `patchesJson6902:
  - target:
      kind: HelmRelease
      name: bundle
    patch: |-
      - op: add
        path: /spec/postRenderers/-
        value:
          kustomize:
            patches:
              - target:
                  kind: Deployment
                patch: |-
                  - op: add
                    path: /metadata/labels/late
                    value: "true"
`

// TestCheckExtrasChecksBundleBlocks: the check renders the kustomization, with
// the blocks and without them, and says what to fix.
func TestCheckExtrasChecksBundleBlocks(t *testing.T) {
	for name, tc := range map[string]struct {
		release, kustomization string
		want                   string // "" means the check passes
	}{
		"create block, no post-renderers":         {bundleRelease, bundleBlock(true), ""},
		"append block, own post-renderers":        {bundleRelease + ownPostRenderer, bundleBlock(false), ""},
		"create block, own post-renderers":        {bundleRelease + ownPostRenderer, bundleBlock(true), "again"},
		"post-renderer after a reservation one":   {bundleRelease + ownPostRenderer, bundleBlock(false) + laterPostRenderer, "after a reservation post-renderer"},
		"create block, post-renderer after block": {bundleRelease, bundleBlock(true) + laterPostRenderer, "after a reservation post-renderer"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "kustomization.yaml")
			for file, content := range map[string]string{
				"release.yaml":       tc.release,
				"kustomization.yaml": "resources:\n  - release.yaml\n" + tc.kustomization,
			} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd, err := New(Config{Stdout: &bytes.Buffer{}})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetArgs([]string{path})
			cmd.SilenceUsage, cmd.SilenceErrors = true, true

			err = cmd.Execute()
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "giantswarm/bundle")):
				t.Errorf("want an error naming the HelmRelease and %q, got %v", tc.want, err)
			}
		})
	}
}
