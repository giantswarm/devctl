package circleci

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci"
)

// Test_detectNodeVersion covers the .nvmrc probe that lets a repo own its Node
// version in one place. The empty results matter as much as the parsed ones:
// every repo predating this probe has no .nvmrc, and .nvmrc also accepts forms
// (aliases, partial versions) that name no cimg/node tag. Both must fall back
// to devctl's baked-in default rather than render an image that cannot be
// pulled.
func Test_detectNodeVersion(t *testing.T) {
	testCases := []struct {
		name string
		// nvmrc is the file content; absent means no .nvmrc is written at all.
		nvmrc  string
		absent bool
		want   string
		// wantRejected is the raw value the caller warns about. It must stay
		// empty whenever there is nothing for a repo owner to act on -- that is
		// what separates "no .nvmrc" (expected) from "unusable .nvmrc" (warn).
		wantRejected string
	}{
		{
			name:   "no .nvmrc keeps the baked-in default silently",
			absent: true,
			want:   "",
		},
		{
			name:  "exact version",
			nvmrc: "24.19.0\n",
			want:  "24.19.0",
		},
		{
			name:  "leading v is stripped",
			nvmrc: "v24.19.0\n",
			want:  "24.19.0",
		},
		{
			name:  "no trailing newline",
			nvmrc: "24.19.0",
			want:  "24.19.0",
		},
		{
			name:  "surrounding whitespace is trimmed",
			nvmrc: "  24.19.0  \n",
			want:  "24.19.0",
		},
		{
			name:  "trailing comment is stripped",
			nvmrc: "24.19.0 # keep in sync with the backend Dockerfile\n",
			want:  "24.19.0",
		},
		{
			name:  "leading comment lines are skipped",
			nvmrc: "# the one source of truth\n24.19.0\n",
			want:  "24.19.0",
		},
		{
			name:  "empty file",
			nvmrc: "",
			want:  "",
		},
		{
			name:  "whitespace-only file",
			nvmrc: "\n\n  \n",
			want:  "",
		},
		{
			name:         "lts alias is rejected and reported",
			nvmrc:        "lts/*\n",
			want:         "",
			wantRejected: "lts/*",
		},
		{
			name:         "node alias is rejected and reported",
			nvmrc:        "node\n",
			want:         "",
			wantRejected: "node",
		},
		{
			name:         "bare major names no cimg/node tag",
			nvmrc:        "24\n",
			want:         "",
			wantRejected: "24",
		},
		{
			// cimg/node:24.19 does exist, so this is rejected on purpose, not
			// because the tag is missing: a floating tag would drift from the
			// exact patch the repo's Dockerfile pins and would coarsen the
			// node-build cache-key salt.
			name:         "major.minor is rejected despite the tag existing",
			nvmrc:        "24.19\n",
			want:         "",
			wantRejected: "24.19",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// detectNodeVersion reads from the working directory, the same as
			// the Dockerfile and lockfile probes beside it.
			dir := t.TempDir()
			if !tc.absent {
				path := filepath.Join(dir, ".nvmrc")
				if err := os.WriteFile(path, []byte(tc.nvmrc), 0o600); err != nil {
					t.Fatalf("write .nvmrc: %v", err)
				}
			}
			t.Chdir(dir)

			got, rejected := detectNodeVersion()
			if got != tc.want {
				t.Errorf("detectNodeVersion() version = %q, want %q", got, tc.want)
			}
			if rejected != tc.wantRejected {
				t.Errorf("detectNodeVersion() rejected = %q, want %q", rejected, tc.wantRejected)
			}
		})
	}
}

// Test_detectATSKindConfig covers the .ats/kind-config.yaml probe: presence is
// the whole signal, and the default -- no file -- is the state of every repo
// predating it.
func Test_detectATSKindConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if detectATSKindConfig() {
		t.Fatal("no .ats/kind-config.yaml must probe false")
	}

	if err := os.MkdirAll(filepath.Join(dir, ".ats"), 0o750); err != nil {
		t.Fatalf("mkdir .ats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, circleci.ATSKindConfigPath), []byte("kind: Cluster\n"), 0o600); err != nil {
		t.Fatalf("write kind config: %v", err)
	}
	if !detectATSKindConfig() {
		t.Fatal(".ats/kind-config.yaml present must probe true")
	}
}

// Test_detectCustomImages covers the custom.yml probe: the image parameters of
// its architect/push-to-registries jobs, sorted, once each; a job without one
// pushes the repo's own image and adds nothing.
func Test_detectCustomImages(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	images, err := detectCustomImages()
	if err != nil || images != nil {
		t.Fatalf("no custom.yml: got %v, %v; want no images", images, err)
	}

	if err := os.MkdirAll(filepath.Join(dir, ".circleci"), 0o750); err != nil {
		t.Fatalf("mkdir .circleci: %v", err)
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, circleci.CustomConfigPath), []byte(content), 0o600); err != nil {
			t.Fatalf("write custom.yml: %v", err)
		}
	}

	write(`version: 2.1
workflows:
  build:
    jobs:
      - fetch-release-notes
      - architect/push-to-registries:
          name: push-second-image
          image: "giantswarm/second-image"
      - architect/push-to-registries:
          name: push-to-registries-latest
      - architect/push-to-app-catalog:
          name: push-chart
          image: giantswarm/not-an-image-job
  release:
    jobs:
      - architect/push-to-registries:
          name: push-debian
          image: giantswarm/debian-variant
      - architect/push-to-registries:
          name: push-second-image-release
          image: giantswarm/second-image
`)
	images, err = detectCustomImages()
	if err != nil {
		t.Fatalf("detectCustomImages: %v", err)
	}
	if want := []string{"giantswarm/debian-variant", "giantswarm/second-image"}; !slices.Equal(images, want) {
		t.Errorf("images = %v, want %v", images, want)
	}

	write("workflows: [\n")
	if _, err := detectCustomImages(); err == nil {
		t.Error("a custom.yml that is no YAML must fail")
	}
}
