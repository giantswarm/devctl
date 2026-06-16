package circleci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

const (
	jobGoBuild        = "architect/go-build"
	jobPushRegistries = "architect/push-to-registries"
	jobSyncChina      = "architect/sync-china-registry"
	jobPushCatalog    = "architect/push-to-app-catalog"
	jobRunTests       = "architect/run-tests-with-ats"

	goldenSetupPath        = "testdata/setup.config.yml"
	goldenWorkflowsPath    = "testdata/mcp-kubernetes.workflows.yml"
	goldenCLIWorkflowsPath = "testdata/mcp-kubernetes.cli.workflows.yml"

	repoMCPKubernetes = "mcp-kubernetes"
	repoSitesearch    = "sitesearch"
)

// mergeExpression is the yq deep-merge the generated setup config runs to
// fold .circleci/custom.yml into .circleci/workflows.yml: maps merge, lists
// (workflow job lists) append. Test_SetupConfigCarriesMergeExpression pins
// this copy to the template so the two cannot drift.
const mergeExpression = `. as $item ireduce ({}; . *+ $item)`

// renderInput executes an input.Input the same way pkg/gen/internal.Execute
// does, returning the bytes that would be written to disk.
func renderInput(t *testing.T, file input.Input) string {
	t.Helper()

	tpl := template.New("config")
	if file.TemplateDelims.Left != "" {
		tpl = tpl.Delims(file.TemplateDelims.Left, file.TemplateDelims.Right)
	}
	tpl, err := tpl.Parse(file.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, file.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	return rendered.String()
}

func newCircleCI(t *testing.T, c Config) *CircleCI {
	t.Helper()

	in, err := New(c)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	return in
}

// render renders the workflows file (.circleci/workflows.yml), which carries
// all derived repo-specific content.
func render(t *testing.T, c Config) string {
	t.Helper()

	return renderInput(t, newCircleCI(t, c).Workflows())
}

func contains(got, substr string) bool {
	return bytes.Contains([]byte(got), []byte(substr))
}

// Test_GoldenSetupConfig is the golden test for the static setup config: it
// carries zero repo-specific content, so one golden covers every repo. Only
// the continuation orb pin varies, and only with devctl releases.
func Test_GoldenSetupConfig(t *testing.T) {
	got := renderInput(t, newCircleCI(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	}).SetupConfig())

	want, err := os.ReadFile(goldenSetupPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated setup config does not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenSetupPath, got, string(want))
	}
}

// Test_SetupConfigIsRepoAgnostic verifies the setup config contains no
// repo-derived content: two repos with entirely different signals must render
// byte-identical setup configs.
func Test_SetupConfigIsRepoAgnostic(t *testing.T) {
	a := renderInput(t, newCircleCI(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile: true,
		BranchPublish: true,
	}).SetupConfig())
	b := renderInput(t, newCircleCI(t, Config{
		RepoName: repoSitesearch,
		Language: gen.Language(""),
		Flavours: gen.FlavourSlice{gen.FlavourApp},
	}).SetupConfig())

	if a != b {
		t.Errorf("setup config must be identical for every repo\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

// Test_SetupConfigCarriesMergeExpression pins the test's copy of the yq merge
// expression to the one in the template, so Test_CustomMerge* keep testing
// what the setup config actually runs.
func Test_SetupConfigCarriesMergeExpression(t *testing.T) {
	got := renderInput(t, newCircleCI(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	}).SetupConfig())

	if !contains(got, mergeExpression) {
		t.Errorf("setup config does not contain the merge expression %q:\n%s", mergeExpression, got)
	}
	if !contains(got, "continuation: circleci/continuation@"+ContinuationOrbVersion) {
		t.Errorf("setup config does not pin continuation orb %s:\n%s", ContinuationOrbVersion, got)
	}
}

// findYq locates a mikefarah yq v4 binary -- the variant cimg/base ships and
// the setup config's merge expression is written for. Some distros package it
// as go-yq, and a plain `yq` may be the incompatible Python jq-wrapper, so
// the version banner is checked.
func findYq(t *testing.T) string {
	t.Helper()

	for _, name := range []string{"yq", "go-yq"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, "--version").CombinedOutput() // #nosec G204 -- fixed args, test-only
		if err == nil && strings.Contains(string(out), "mikefarah") {
			return path
		}
	}

	t.Skip("mikefarah yq v4 not installed; skipping merge test")
	return ""
}

// yqMerge runs the setup config's merge expression over workflows.yml +
// custom.yml the same way the setup job does, returning the merged config.
func yqMerge(t *testing.T, workflows, custom string) string {
	t.Helper()

	yq := findYq(t)

	dir := t.TempDir()
	workflowsPath := filepath.Join(dir, "workflows.yml")
	customPath := filepath.Join(dir, "custom.yml")
	if err := os.WriteFile(workflowsPath, []byte(workflows), 0600); err != nil { // #nosec G703 -- t.TempDir() path, test-only
		t.Fatalf("write workflows.yml: %v", err)
	}
	if err := os.WriteFile(customPath, []byte(custom), 0600); err != nil {
		t.Fatalf("write custom.yml: %v", err)
	}

	var out, stderr bytes.Buffer
	cmd := exec.Command(yq, "eval-all", mergeExpression, workflowsPath, customPath) // #nosec G204 -- fixed args, test-only
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("yq merge failed: %v\nstderr: %s", err, stderr.String())
	}

	return out.String()
}

// yqQuery evaluates a yq expression against a YAML document and returns the
// trimmed result.
func yqQuery(t *testing.T, doc, expr string) string {
	t.Helper()

	yq := findYq(t)

	var out, stderr bytes.Buffer
	cmd := exec.Command(yq, "eval", expr, "-") // #nosec G204 -- fixed args, test-only
	cmd.Stdin = strings.NewReader(doc)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("yq query %q failed: %v\nstderr: %s", expr, err, stderr.String())
	}

	return strings.TrimSpace(out.String())
}

// customFixture is a representative custom.yml: a repo-owned e2e job with its
// own job definition and a workflow entry appended into the generated build
// workflow, requiring a generated job by its bare name.
const customFixture = `jobs:
  e2e-smoke:
    machine:
      image: ubuntu-2404:current
    steps:
    - checkout
    - run: make e2e

workflows:
  build:
    jobs:
    - e2e-smoke:
        requires:
        - go-build
        filters:
          tags:
            only: /^v.*/
`

// Test_CustomMergeAppendsJobs runs the setup config's yq expression over the
// service golden + a custom.yml fixture and verifies the merge contract: the
// custom job definition lands in .jobs (map merge), the custom workflow entry
// is appended to the generated build workflow's job list (list append), and
// the generated content is untouched.
func Test_CustomMergeAppendsJobs(t *testing.T) {
	workflows, err := os.ReadFile(goldenWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	merged := yqMerge(t, string(workflows), customFixture)

	baseJobs := yqQuery(t, string(workflows), ".workflows.build.jobs | length")
	mergedJobs := yqQuery(t, merged, ".workflows.build.jobs | length")
	if baseJobs == mergedJobs {
		t.Errorf("custom workflow entry was not appended: %s jobs before and after merge", mergedJobs)
	}

	if got := yqQuery(t, merged, `.workflows.build.jobs[-1] | keys | .[0]`); got != "e2e-smoke" {
		t.Errorf("expected custom job appended last to build workflow, got %q", got)
	}
	if got := yqQuery(t, merged, `.jobs.e2e-smoke.machine.image`); got != "ubuntu-2404:current" {
		t.Errorf("custom job definition not merged into .jobs, got image %q", got)
	}
	if got := yqQuery(t, merged, `.orbs.architect`); !strings.HasPrefix(got, "giantswarm/architect@") {
		t.Errorf("generated orbs map damaged by merge, got %q", got)
	}
	if got := yqQuery(t, merged, `.version`); got != "2.1" {
		t.Errorf("version damaged by merge, got %q", got)
	}
	if got := yqQuery(t, merged, `[.workflows.build.jobs[] | select(has("architect/go-build"))] | length`); got != "1" {
		t.Errorf("generated go-build entry damaged by merge, got %s entries", got)
	}
}

// Test_CustomMergeOwnWorkflow verifies a custom.yml that adds its own workflow
// (e.g. a nightly cron) merges as a sibling of the generated build workflow.
func Test_CustomMergeOwnWorkflow(t *testing.T) {
	workflows, err := os.ReadFile(goldenWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	custom := `workflows:
  nightly:
    triggers:
    - schedule:
        cron: "0 3 * * *"
        filters:
          branches:
            only:
            - main
    jobs:
    - architect/go-build:
        name: go-build
        binary: mcp-kubernetes
        context: architect
`

	merged := yqMerge(t, string(workflows), custom)

	if got := yqQuery(t, merged, ".workflows | keys | length"); got != "2" {
		t.Errorf("expected build + nightly workflows after merge, got %s", got)
	}
	if got := yqQuery(t, merged, `.workflows.nightly.triggers[0].schedule.cron`); got != "0 3 * * *" {
		t.Errorf("nightly cron not merged, got %q", got)
	}
}

// Test_GoldenServiceWorkflows is the golden test: generating with
// mcp-kubernetes's signals (language go, app flavour, a Dockerfile,
// branch-publish off) must reproduce the aligned standard byte-for-byte. The
// golden reflects the build+test-only branch default: branches run go-build,
// build-chart, and execute-chart-tests, while the image and chart pushes are
// tag-only.
func Test_GoldenServiceWorkflows(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})

	want, err := os.ReadFile(goldenWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenWorkflowsPath, got, string(want))
	}
}

// Test_GoldenCLIWorkflows is the golden test for the cli-flavour shape: a Go
// repo that also carries the cli flavour ships cross-platform binaries on its
// GitHub Release. Generating must reproduce the aligned standard
// byte-for-byte (the six-platform architectures on go-build, the
// upload-release-assets job, and the capped release image push).
func Test_GoldenCLIWorkflows(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile: true,
	})

	want, err := os.ReadFile(goldenCLIWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenCLIWorkflowsPath, got, string(want))
	}
}

// Test_AppCatalogOverride verifies the chart pipeline publishes to the
// overridden catalog when set, and falls back to the public defaults when not.
// Repos on the internal giantswarm-operations-platform catalog rely on this so
// generation does not silently migrate their chart to the public catalog.
func Test_AppCatalogOverride(t *testing.T) {
	got := render(t, Config{
		RepoName:       repoSitesearch,
		Flavours:       gen.FlavourSlice{gen.FlavourApp},
		AppCatalog:     "giantswarm-operations-platform-catalog",
		AppCatalogTest: "giantswarm-operations-platform-test-catalog",
	})

	if !contains(got, "app_catalog: giantswarm-operations-platform-catalog") {
		t.Errorf("override not applied; want app_catalog override in:\n%s", got)
	}
	if !contains(got, "app_catalog_test: giantswarm-operations-platform-test-catalog") {
		t.Errorf("override not applied; want app_catalog_test override in:\n%s", got)
	}
	if contains(got, "app_catalog: giantswarm-catalog") {
		t.Errorf("default catalog leaked through despite override:\n%s", got)
	}

	def := render(t, Config{
		RepoName: repoSitesearch,
		Flavours: gen.FlavourSlice{gen.FlavourApp},
	})
	if !contains(def, "app_catalog: "+DefaultAppCatalog) {
		t.Errorf("empty override should default to %q:\n%s", DefaultAppCatalog, def)
	}
	if !contains(def, "app_catalog_test: "+DefaultAppCatalogTest) {
		t.Errorf("empty override should default to %q:\n%s", DefaultAppCatalogTest, def)
	}
}

// Test_ImagePreBuildJob verifies the release image build gains a requires
// entry for the named repo-owned pre-build job (a workspace-handoff pre-step
// the append-only custom.yml merge cannot inject into a generated job), and
// that omitting it leaves the release job's requires untouched.
func Test_ImagePreBuildJob(t *testing.T) {
	got := render(t, Config{
		RepoName:         "agentic-platform-ui",
		Flavours:         gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile:    true,
		ImagePreBuildJob: "fetch-release-notes",
	})

	// The release image job must require the custom pre-build job.
	if !contains(got, "- fetch-release-notes") {
		t.Errorf("release image job missing requires on pre-build job:\n%s", got)
	}

	def := render(t, Config{
		RepoName:      "agentic-platform-ui",
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	if contains(def, "- fetch-release-notes") {
		t.Errorf("pre-build requires leaked without ImagePreBuildJob:\n%s", def)
	}
}

// Test_ImagePrivateOnly verifies a private-only image build pushes to the
// private registry via registries-data and omits split-china-push and the
// sync-china-registry job, while the default keeps the public split-china shape.
func Test_ImagePrivateOnly(t *testing.T) {
	got := render(t, Config{
		RepoName:         "agentic-platform-ui",
		Flavours:         gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile:    true,
		ImagePrivateOnly: true,
	})

	if !contains(got, "registries-data: |-") {
		t.Errorf("private-only image missing registries-data:\n%s", got)
	}
	if !contains(got, "private gsociprivate.azurecr.io") {
		t.Errorf("private-only image missing private registry target:\n%s", got)
	}
	if contains(got, "split-china-push: true") {
		t.Errorf("private-only image should not use split-china-push:\n%s", got)
	}
	if contains(got, jobSyncChina) {
		t.Errorf("private-only image should omit sync-china-registry:\n%s", got)
	}

	def := render(t, Config{
		RepoName:      "agentic-platform-ui",
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	if !contains(def, "split-china-push: true") {
		t.Errorf("default image should use split-china-push:\n%s", def)
	}
	if !contains(def, jobSyncChina) {
		t.Errorf("default image should include sync-china-registry:\n%s", def)
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
		t.Errorf("expected generated workflows to pin orb %s, got:\n%s", OrbVersion, got)
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
		RepoName:      repoSitesearch,
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
// branches build + test only, plus the push-less image validation. The branch
// image push (name: push-to-registries) and the branch chart push (name:
// push-chart) must be absent, while the build-only image validation (name:
// build-image with push: false), the tag-only release jobs, and the shared
// build-chart job remain.
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
		"name: build-image",
		"push: false",
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
// push (name: push-chart), without disturbing the tag-only release jobs. The
// push-less build-image validation is omitted -- the branch image push already
// exercises the Dockerfile.
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
	for _, unwanted := range []string{
		"name: build-image",
		"push: false",
	} {
		if contains(got, unwanted) {
			t.Errorf("branch-publish config should not contain build-only validation %q:\n%s", unwanted, got)
		}
	}
}

// Test_NoCLIOmitsReleaseBinaries verifies the default: a Go service/chart repo
// without the cli flavour carries no architectures matrix, no
// upload-release-assets job, and no platforms cap on the release image push.
func Test_NoCLIOmitsReleaseBinaries(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})

	for _, unwanted := range []string{
		"architectures:",
		"name: upload-release-assets",
		"platforms: \"linux/amd64,linux/arm64\"",
	} {
		if contains(got, unwanted) {
			t.Errorf("non-cli config should not contain release-binaries %q:\n%s", unwanted, got)
		}
	}
}

// Test_CLIAddsReleaseBinaries verifies the derivation: the cli flavour on a Go
// repo emits the six-platform architectures matrix on go-build, an
// upload-release-assets job (tag-only), and caps the multi-arch release image
// push to the two linux platforms so buildx does not try darwin/windows under
// QEMU.
func Test_CLIAddsReleaseBinaries(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile: true,
	})

	for _, want := range []string{
		`architectures: "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64"`,
		"name: upload-release-assets",
		`platforms: "linux/amd64,linux/arm64"`,
	} {
		if !contains(got, want) {
			t.Errorf("cli config missing %q:\n%s", want, got)
		}
	}
}

// Test_CLIWithoutGoOmitsReleaseBinaries verifies the Go guard: the cli flavour
// on a repo with no Go build never emits the binary jobs (there would be no
// binary to upload).
func Test_CLIWithoutGoOmitsReleaseBinaries(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoSitesearch,
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile: false,
	})

	if contains(got, "name: upload-release-assets") {
		t.Errorf("non-go config should not contain upload-release-assets:\n%s", got)
	}
}
