package circleci

import (
	"bytes"
	"os"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v7/pkg/gen"
)

const (
	jobGoBuild        = "architect/go-build"
	jobPushRegistries = "architect/push-to-registries"
	jobSyncChina      = "architect/sync-china-registry"
	jobPushCatalog    = "architect/push-to-app-catalog"
	jobRunTests       = "architect/run-tests-with-ats"

	goldenPath = "testdata/mcp-kubernetes.config.yml"
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
// signals (language go, app flavour, a Dockerfile) must reproduce the aligned
// standard byte-for-byte. The golden file is mcp-kubernetes's checked-in
// .circleci/config.yml with the only allowed drift -- the orb bump to the
// aligned 8.3.0 -- applied.
func Test_GoldenServiceConfig(t *testing.T) {
	got := render(t, Config{
		RepoName:      "mcp-kubernetes",
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

func Test_DefaultOrbVersion(t *testing.T) {
	got := render(t, Config{
		RepoName:      "mcp-kubernetes",
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})

	if !contains(got, "giantswarm/architect@"+DefaultOrbVersion) {
		t.Errorf("expected generated config to pin orb %s, got:\n%s", DefaultOrbVersion, got)
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
