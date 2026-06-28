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

	goldenNodeNPMPath       = "testdata/node-npm.workflows.yml"
	goldenNodeYarnBerryPath = "testdata/node-yarn-berry.workflows.yml"

	repoMCPKubernetes = "mcp-kubernetes"
	repoSitesearch    = "sitesearch"
	repoK8sTypes      = "k8s-typescript-types"
	repoBackstage     = "backstage"

	backstageDockerfile  = "packages/backend/Dockerfile"
	backstageBuildOutput = "packages/*/dist/*"
	nodeBuildTarget      = "build:backend"
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

// Test_CLIParallelBuild verifies the cli flavour (six-arch cross-compile)
// gets the orb's build_concurrency + a larger resource_class so the cold
// post-go.sum-bump build parallelises, and that a non-cli Go service does not
// pay for the larger box (the knobs live in the ReleaseBinaries block).
func Test_CLIParallelBuild(t *testing.T) {
	cli := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile: true,
	})
	if !contains(cli, "build_concurrency: auto") {
		t.Errorf("cli flavour missing build_concurrency: auto:\n%s", cli)
	}
	if !contains(cli, "resource_class: large") {
		t.Errorf("cli flavour missing resource_class: large:\n%s", cli)
	}

	svc := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	if contains(svc, "build_concurrency") {
		t.Errorf("non-cli service should not set build_concurrency:\n%s", svc)
	}
	if contains(svc, "resource_class") {
		t.Errorf("non-cli service should not set resource_class:\n%s", svc)
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

	// Both the branch validation (build-image) and the release push must
	// require the custom pre-build job: the branch build compiles the same
	// Dockerfile and needs the same workspace handoff.
	if n := strings.Count(got, "- fetch-release-notes"); n != 2 {
		t.Errorf("expected pre-build requires on build-image and release push, found %d:\n%s", n, got)
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

// Test_ImageName verifies the image-name override is applied to every image
// job (the branch validation, the release push, and the sync-china-registry
// mirror) so a repo whose published image differs from its repo name (e.g.
// kserve -> giantswarm/kserve-controller) is generated correctly, and that
// omitting it leaves the orb's giantswarm/<repo> default in place (no image
// param emitted).
func Test_ImageName(t *testing.T) {
	got := render(t, Config{
		RepoName:      "kserve",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{},
		HasDockerfile: true,
		ImageName:     "giantswarm/kserve-controller",
	})

	// Every image job (build-image, push-to-registries-release,
	// sync-china-registry) must carry the overridden image name.
	if n := strings.Count(got, "image: giantswarm/kserve-controller"); n != 3 {
		t.Errorf("expected image override on all 3 image jobs, found %d:\n%s", n, got)
	}

	def := render(t, Config{
		RepoName:      "kserve",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{},
		HasDockerfile: true,
	})
	if contains(def, "image:") {
		t.Errorf("no image param should be emitted without ImageName (orb default applies):\n%s", def)
	}
}

// Test_ImagePlatforms verifies the platform override caps the buildx platform
// list on both the branch validation (build-image) and the release push for a
// single-architecture image (e.g. vllm -> linux/arm64), and that omitting it
// emits no platforms param (the orb falls back to its default).
func Test_ImagePlatforms(t *testing.T) {
	got := render(t, Config{
		RepoName:       "vllm",
		Language:       gen.Language(""),
		Flavours:       gen.FlavourSlice{},
		HasDockerfile:  true,
		ImagePlatforms: "linux/arm64",
	})

	// build-image (branch) and push-to-registries-release (tag) must both
	// carry the single-arch cap.
	if n := strings.Count(got, "platforms: linux/arm64"); n != 2 {
		t.Errorf("expected platforms cap on build-image and release push, found %d:\n%s", n, got)
	}

	def := render(t, Config{
		RepoName:      "vllm",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{},
		HasDockerfile: true,
	})
	if contains(def, "platforms:") {
		t.Errorf("no platforms param should be emitted without ImagePlatforms (orb default applies):\n%s", def)
	}
}

// Test_ImageDockerfile verifies the dockerfile-path override turns the image
// pipeline on even when no root Dockerfile is detected (HasDockerfile false)
// and applies the path to the build jobs (build-image and
// push-to-registries-release; the sync-china-registry mirror does not build).
// This is the backstage shape: a chart repo (app flavour, generic language)
// whose image is built from packages/backend/Dockerfile. Omitting it emits no
// dockerfile param so the orb default applies.
func Test_ImageDockerfile(t *testing.T) {
	got := render(t, Config{
		RepoName:        "backstage",
		Language:        gen.Language(""),
		Flavours:        gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile:   false,
		ImageDockerfile: "packages/backend/Dockerfile",
	})

	// The image pipeline must be generated despite HasDockerfile=false.
	if !contains(got, "name: push-to-registries-release") {
		t.Errorf("ImageDockerfile did not turn the image pipeline on:\n%s", got)
	}
	// build-image (branch) and push-to-registries-release (tag) carry the path;
	// sync-china-registry mirrors and does not build, so it must not.
	if n := strings.Count(got, "dockerfile: packages/backend/Dockerfile"); n != 2 {
		t.Errorf("expected dockerfile path on build-image and release push, found %d:\n%s", n, got)
	}

	def := render(t, Config{
		RepoName:      "backstage",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	if contains(def, "dockerfile:") {
		t.Errorf("no dockerfile param should be emitted without ImageDockerfile (orb default applies):\n%s", def)
	}
}

// Test_ChartName verifies the chart-name override is applied to every chart
// job (build-chart and the tag-only push-chart-release) for a repo whose chart
// directory does not match the repo name (e.g. docs-proxy ships
// helm/docs-proxy-app), and that omitting it falls back to the repo name.
func Test_ChartName(t *testing.T) {
	got := render(t, Config{
		RepoName:      "docs-proxy",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		ChartName:     "docs-proxy-app",
	})

	// build-chart and push-chart-release must both carry the chart name.
	if n := strings.Count(got, "chart: docs-proxy-app"); n != 2 {
		t.Errorf("expected chart-name override on build-chart and push-chart-release, found %d:\n%s", n, got)
	}
	if contains(got, "chart: docs-proxy\n") {
		t.Errorf("repo name leaked through despite chart-name override:\n%s", got)
	}

	def := render(t, Config{
		RepoName:      "docs-proxy",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	if !contains(def, "chart: docs-proxy\n") {
		t.Errorf("empty chart-name should default to the repo name:\n%s", def)
	}
}

// Test_ForcePublic verifies that force-public: true lands on the release image
// push and the release chart push for a private repo that publishes public
// artifacts (e.g. web-assets), and that the default emits no force-public.
func Test_ForcePublic(t *testing.T) {
	got := render(t, Config{
		RepoName:      "web-assets",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		ForcePublic:   true,
	})

	// push-to-registries-release (image) and push-chart-release (chart) must
	// both force the public push.
	if n := strings.Count(got, "force-public: true"); n != 2 {
		t.Errorf("expected force-public on the release image and chart pushes, found %d:\n%s", n, got)
	}

	def := render(t, Config{
		RepoName:      "web-assets",
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	if contains(def, "force-public: true") {
		t.Errorf("force-public leaked without ForcePublic:\n%s", def)
	}
}

// Test_ForcePublicPrivateOnlyConflict verifies the two mutually-exclusive
// registry-scope knobs are rejected when both are set (one forces public, the
// other forces private).
func Test_ForcePublicPrivateOnlyConflict(t *testing.T) {
	_, err := New(Config{
		RepoName:         "web-assets",
		Flavours:         gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile:    true,
		ForcePublic:      true,
		ImagePrivateOnly: true,
	})
	if !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for ForcePublic+ImagePrivateOnly, got %v", err)
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

// Test_GoBuildTestTarget verifies the go-build job routes unit tests through
// the `make test` target (architect test_target) so CI and local agent runs
// share one command. The generic Makefile target is `go test ./...`; per-repo
// Makefiles override it for -race, integration suites, etc. (make-target CI
// interface). A repo with no Go build emits no go-build job and thus no
// test_target.
func Test_GoBuildTestTarget(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	if !contains(got, "test_target: test") {
		t.Errorf("go-build job missing test_target: test:\n%s", got)
	}

	chartOnly := render(t, Config{
		RepoName:      repoSitesearch,
		Language:      gen.Language(""),
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: false,
	})
	if contains(chartOnly, "test_target") {
		t.Errorf("non-go config should not emit test_target:\n%s", chartOnly)
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

// Test_GoldenNodeNPMWorkflows is the golden test for a Node library on npm with
// no image or chart (the k8s-typescript-types shape, AC: node-test only): a
// self-contained node-test job on cimg/node with an npm-keyed dependency cache
// and the default `npm run test` verify, and no architect image/chart jobs.
func Test_GoldenNodeNPMWorkflows(t *testing.T) {
	got := render(t, Config{
		RepoName:       repoK8sTypes,
		Language:       gen.LanguageNode,
		PackageManager: PackageManagerNPM,
	})

	want, err := os.ReadFile(goldenNodeNPMPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenNodeNPMPath, got, string(want))
	}
}

// Test_GoldenNodeYarnBerryWorkflows is the golden test for the backstage shape
// (AC: Yarn-Berry + gen.ci.image.preBuildJob): a node-build job on cimg/node
// with a Yarn-Berry-keyed cache, a configurable build target, and a persisted
// build output, feeding an image (non-root Dockerfile) and a chart whose image
// and chart jobs gate on node-build.
func Test_GoldenNodeYarnBerryWorkflows(t *testing.T) {
	got := render(t, Config{
		RepoName:        repoBackstage,
		Language:        gen.LanguageNode,
		Flavours:        gen.FlavourSlice{gen.FlavourApp},
		PackageManager:  PackageManagerYarn,
		NodeBuildTarget: nodeBuildTarget,
		NodeBuildOutput: backstageBuildOutput,
		ImageDockerfile: backstageDockerfile,
	})

	want, err := os.ReadFile(goldenNodeYarnBerryPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenNodeYarnBerryPath, got, string(want))
	}
}

// Test_NodeLibraryNeedsNoOtherSignal verifies the relaxed no-jobs guard: a Node
// repo with no Dockerfile and no app flavour is a valid config (the node-test
// job is the signal), where the same shape without a language is rejected.
func Test_NodeLibraryNeedsNoOtherSignal(t *testing.T) {
	got := render(t, Config{
		RepoName:       repoK8sTypes,
		Language:       gen.LanguageNode,
		PackageManager: PackageManagerNPM,
	})
	if !contains(got, "node-test:") {
		t.Errorf("Node library missing node-test job:\n%s", got)
	}

	_, err := New(Config{
		RepoName: repoK8sTypes,
		Language: gen.Language(""),
	})
	if !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for a languageless repo with no other signal, got %v", err)
	}
}

// Test_NodeJobNameFromBuildOutput verifies the job is named node-build and
// persists its workspace when a build output is set (the image-feeding shape),
// and is named node-test with no persist_to_workspace otherwise.
func Test_NodeJobNameFromBuildOutput(t *testing.T) {
	build := render(t, Config{
		RepoName:        repoBackstage,
		Language:        gen.LanguageNode,
		PackageManager:  PackageManagerYarn,
		NodeBuildTarget: nodeBuildTarget,
		NodeBuildOutput: backstageBuildOutput,
		ImageDockerfile: backstageDockerfile,
	})
	if !contains(build, "node-build:") {
		t.Errorf("build output should name the job node-build:\n%s", build)
	}
	if !contains(build, "persist_to_workspace:") {
		t.Errorf("build output should persist the workspace:\n%s", build)
	}
	if !contains(build, "- "+backstageBuildOutput) {
		t.Errorf("persisted path missing the build output:\n%s", build)
	}

	test := render(t, Config{
		RepoName:       repoK8sTypes,
		Language:       gen.LanguageNode,
		PackageManager: PackageManagerNPM,
	})
	if !contains(test, "node-test:") {
		t.Errorf("no build output should name the job node-test:\n%s", test)
	}
	if contains(test, "persist_to_workspace:") {
		t.Errorf("node-test should not persist a workspace:\n%s", test)
	}
}

// Test_NodeImageGatesOnBuildJob verifies the generalized requires wiring: a
// Node repo feeding an image and a chart gates the image jobs (build-image,
// push-to-registries-release) and the chart job (build-chart) on node-build,
// the same way a Go repo gates them on go-build.
func Test_NodeImageGatesOnBuildJob(t *testing.T) {
	got := render(t, Config{
		RepoName:        repoBackstage,
		Language:        gen.LanguageNode,
		Flavours:        gen.FlavourSlice{gen.FlavourApp},
		PackageManager:  PackageManagerYarn,
		NodeBuildTarget: nodeBuildTarget,
		NodeBuildOutput: backstageBuildOutput,
		ImageDockerfile: backstageDockerfile,
	})

	// build-image, push-to-registries-release, and build-chart each gate on
	// node-build via a `- node-build` requires entry.
	if n := strings.Count(got, "- node-build\n"); n != 3 {
		t.Errorf("expected 3 requires entries on node-build (build-image, release push, build-chart), found %d:\n%s", n, got)
	}
	if contains(got, "- go-build") {
		t.Errorf("Node repo should not reference go-build:\n%s", got)
	}
}

// Test_NodePreBuildJobCoexists verifies gen.ci.image.preBuildJob still works
// with a Node build job: the image jobs require both node-build and the
// repo-owned pre-build job.
func Test_NodePreBuildJobCoexists(t *testing.T) {
	got := render(t, Config{
		RepoName:         repoBackstage,
		Language:         gen.LanguageNode,
		PackageManager:   PackageManagerYarn,
		NodeBuildTarget:  nodeBuildTarget,
		NodeBuildOutput:  backstageBuildOutput,
		ImageDockerfile:  backstageDockerfile,
		ImagePreBuildJob: "fetch-release-notes",
	})

	if !contains(got, "- node-build\n") {
		t.Errorf("image jobs should require node-build:\n%s", got)
	}
	if n := strings.Count(got, "- fetch-release-notes"); n != 2 {
		t.Errorf("expected pre-build requires on build-image and release push, found %d:\n%s", n, got)
	}
}

// Test_NodePackageManagers verifies each detected package manager renders its
// own install command, cache path, and lockfile-keyed cache key, and that only
// pnpm activates corepack.
func Test_NodePackageManagers(t *testing.T) {
	cases := []struct {
		pm          string
		install     string
		cachePath   string
		cacheKey    string
		wantCorepak bool
	}{
		{PackageManagerNPM, "npm ci", "~/.npm", `node-deps-npm-{{ checksum "package-lock.json" }}`, false},
		{PackageManagerYarn, "yarn install --immutable", ".yarn/cache", `node-deps-yarn-{{ checksum "yarn.lock" }}`, false},
		{PackageManagerYarnClassic, "yarn install --frozen-lockfile", "~/.cache/yarn", `node-deps-yarn-classic-{{ checksum "yarn.lock" }}`, false},
		{PackageManagerPNPM, "pnpm install --frozen-lockfile", "~/.local/share/pnpm/store", `node-deps-pnpm-{{ checksum "pnpm-lock.yaml" }}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.pm, func(t *testing.T) {
			got := render(t, Config{
				RepoName:       repoK8sTypes,
				Language:       gen.LanguageNode,
				PackageManager: tc.pm,
			})
			if !contains(got, "command: "+tc.install) {
				t.Errorf("%s missing install command %q:\n%s", tc.pm, tc.install, got)
			}
			if !contains(got, "- "+tc.cachePath) {
				t.Errorf("%s missing cache path %q:\n%s", tc.pm, tc.cachePath, got)
			}
			if !contains(got, tc.cacheKey) {
				t.Errorf("%s missing cache key %q:\n%s", tc.pm, tc.cacheKey, got)
			}
			corepack := contains(got, "corepack enable")
			if corepack != tc.wantCorepak {
				t.Errorf("%s corepack = %v, want %v:\n%s", tc.pm, corepack, tc.wantCorepak, got)
			}
		})
	}
}

// Test_NodeTestTargetConfigurable verifies the verify step runs the default
// `test` script and an override redirects it (the make-target interface),
// while the build step is omitted unless a build target is set.
func Test_NodeTestTargetConfigurable(t *testing.T) {
	def := render(t, Config{
		RepoName:       repoK8sTypes,
		Language:       gen.LanguageNode,
		PackageManager: PackageManagerNPM,
	})
	if !contains(def, "command: npm run "+DefaultNodeTestTarget) {
		t.Errorf("default verify should run %q:\n%s", DefaultNodeTestTarget, def)
	}
	if contains(def, "name: Build") {
		t.Errorf("no build target should omit the Build step:\n%s", def)
	}

	override := render(t, Config{
		RepoName:        repoK8sTypes,
		Language:        gen.LanguageNode,
		PackageManager:  PackageManagerNPM,
		NodeTestTarget:  "ci:verify",
		NodeBuildTarget: "compile",
	})
	if !contains(override, "command: npm run ci:verify") {
		t.Errorf("verify override not applied:\n%s", override)
	}
	if !contains(override, "command: npm run compile") {
		t.Errorf("build target not applied:\n%s", override)
	}
}

// Test_GoUnaffectedByBuildJobName is a regression guard: generalizing the
// image/chart requires wiring to BuildJobName must keep the Go path gating on
// go-build exactly as before.
func Test_GoUnaffectedByBuildJobName(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	})
	// build-image, push-to-registries-release, and build-chart gate on go-build.
	if n := strings.Count(got, "- go-build\n"); n != 3 {
		t.Errorf("expected 3 go-build requires entries, found %d:\n%s", n, got)
	}
	if contains(got, "- node-build") {
		t.Errorf("Go repo should not reference node-build:\n%s", got)
	}
}
