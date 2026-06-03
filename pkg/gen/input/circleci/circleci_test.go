package circleci

import (
	"bytes"
	"os"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	jobGoBuild        = "architect/go-build"
	jobPushRegistries = "architect/push-to-registries"
	jobSyncChina      = "architect/sync-china-registry"
	jobPushCatalog    = "architect/push-to-app-catalog"
	jobRunTests       = "architect/run-tests-with-ats"

	goldenPath = "testdata/mcp-kubernetes.config.yml"

	repoMCPKubernetes = "mcp-kubernetes"
)

// render executes an input.Input the same way pkg/gen/internal.Execute does,
// returning the bytes that would be written to disk.
func render(t *testing.T, c Config) string {
	t.Helper()

	in, err := New(c)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	file := in.Config()

	tpl := template.New("config")
	if file.TemplateDelims.Left != "" {
		tpl = tpl.Delims(file.TemplateDelims.Left, file.TemplateDelims.Right)
	}
	tpl, err = tpl.Parse(file.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, file.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	return rendered.String()
}

func contains(got, substr string) bool {
	return bytes.Contains([]byte(got), []byte(substr))
}

// Test_GoldenServiceConfig is the golden test: generating with mcp-kubernetes's
// signals (language go, app flavour, a Dockerfile, branch-publish off) must
// reproduce the aligned standard byte-for-byte. The golden reflects the
// build+test-only branch default: branches run go-build, build-chart, and
// execute-chart-tests, while the image and chart pushes are tag-only.
func Test_GoldenServiceConfig(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})

	want, err := os.ReadFile(goldenPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated config does not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, string(want))
	}
}

func Test_OrbVersion(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})

	if !contains(got, "giantswarm/architect@"+OrbVersion) {
		t.Errorf("expected generated config to pin orb %s, got:\n%s", OrbVersion, got)
	}
}

// Test_ImageOnlyOmitsChart verifies derivation: an image repo without the app
// flavour gets the image pipeline but no chart jobs.
func Test_ImageOnlyOmitsChart(t *testing.T) {
	got := render(t, Config{
		RepoName:      "crd-docs-generator",
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{},
		HasDockerfile: true,
	})

	for _, want := range []string{jobGoBuild, jobPushRegistries, jobSyncChina} {
		if !contains(got, want) {
			t.Errorf("image config missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{jobPushCatalog, jobRunTests} {
		if contains(got, unwanted) {
			t.Errorf("image config should not contain %q:\n%s", unwanted, got)
		}
	}
}

// Test_BinaryOnlyEmitsGoBuildAlone verifies derivation: a Go repo with no
// Dockerfile and no app flavour emits the go-build job and nothing else.
func Test_BinaryOnlyEmitsGoBuildAlone(t *testing.T) {
	got := render(t, Config{
		RepoName:      "klausctl",
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{},
		HasDockerfile: false,
	})

	if !contains(got, jobGoBuild) {
		t.Errorf("binary config missing %q:\n%s", jobGoBuild, got)
	}
	for _, unwanted := range []string{jobPushRegistries, jobSyncChina, jobPushCatalog, jobRunTests} {
		if contains(got, unwanted) {
			t.Errorf("binary config should not contain %q:\n%s", unwanted, got)
		}
	}
}

// Test_NoSignalsRejected verifies that a config with no language, Dockerfile,
// or app flavour is rejected rather than rendering an empty jobs list.
func Test_NoSignalsRejected(t *testing.T) {
	_, err := New(Config{
		RepoName:      "foo",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{},
		HasDockerfile: false,
	})
	if !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError, got %v", err)
	}
}

// Test_ChartOnlyOmitsImage verifies derivation: a chart repo without a
// Dockerfile gets the chart pipeline but no image jobs, and the chart push
// requires drop the image-job references.
func Test_ChartOnlyOmitsImage(t *testing.T) {
	got := render(t, Config{
		RepoName:      "sitesearch",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: false,
	})

	for _, want := range []string{jobPushCatalog, jobRunTests} {
		if !contains(got, want) {
			t.Errorf("chart config missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{jobGoBuild, jobPushRegistries, jobSyncChina, "- push-to-registries"} {
		if contains(got, unwanted) {
			t.Errorf("chart config should not contain %q:\n%s", unwanted, got)
		}
	}
}

// Test_BranchPublishOffOmitsBranchPushes verifies the default branch shape:
// branches build + test only. The branch image push (name: push-to-registries)
// and the branch chart push (name: push-chart) must be absent, while the
// tag-only release jobs and the shared build-chart job remain.
func Test_BranchPublishOffOmitsBranchPushes(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		BranchPublish: false,
	})

	for _, want := range []string{
		"name: go-build",
		"name: build-chart",
		"name: execute-chart-tests",
		"name: push-to-registries-release",
		"name: sync-china-registry",
		"name: push-chart-release",
	} {
		if !contains(got, want) {
			t.Errorf("default config missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"name: push-to-registries\n",
		"name: push-chart\n",
		"platforms: linux/amd64",
	} {
		if contains(got, unwanted) {
			t.Errorf("default config should not contain branch-publish %q:\n%s", unwanted, got)
		}
	}
}

// Test_BranchPublishOnAddsCoupledBranchPushes verifies the opt-in branch shape:
// the branch path additionally emits an amd64 image push (name:
// push-to-registries with platforms: linux/amd64) and the coupled branch chart
// push (name: push-chart), without disturbing the tag-only release jobs.
func Test_BranchPublishOnAddsCoupledBranchPushes(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		BranchPublish: true,
	})

	for _, want := range []string{
		"name: push-to-registries\n",
		"platforms: linux/amd64",
		"name: push-chart\n",
		"name: push-to-registries-release",
		"name: push-chart-release",
	} {
		if !contains(got, want) {
			t.Errorf("branch-publish config missing %q:\n%s", want, got)
		}
	}
}
