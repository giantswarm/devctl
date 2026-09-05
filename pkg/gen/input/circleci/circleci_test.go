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

	goldenNativeWorkflowsPath     = "testdata/mcp-kubernetes.native.workflows.yml"
	goldenNativeNodeWorkflowsPath = "testdata/node-yarn-berry.native.workflows.yml"

	goldenChartOnlyWorkflowsPath    = "testdata/agent-platform-standalone.workflows.yml"
	goldenATSOnReleaseWorkflowsPath = "testdata/mcp-kubernetes.ats-on-release.workflows.yml"
	goldenATSOnePointXWorkflowsPath = "testdata/agent-platform-standalone.ats-1.workflows.yml"

	goldenGoTestArtifactsWorkflowsPath = "testdata/muster.workflows.yml"

	repoMCPKubernetes = "mcp-kubernetes"
	repoAPStandalone  = "agent-platform-standalone"
	repoSitesearch    = "sitesearch"
	repoK8sTypes      = "k8s-typescript-types"
	repoBackstage     = "backstage"

	backstageDockerfile  = "packages/backend/Dockerfile"
	backstageBuildOutput = "packages/*/dist/*"
	nodeBuildTarget      = "build:backend"

	resourceClassXLarge = "xlarge"
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
	if !contains(cli, "build_concurrency: "+DefaultBuildConcurrency) {
		t.Errorf("cli flavour missing build_concurrency: %s:\n%s", DefaultBuildConcurrency, cli)
	}
	if !contains(cli, "resource_class: "+DefaultResourceClass) {
		t.Errorf("cli flavour missing resource_class: %s:\n%s", DefaultResourceClass, cli)
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

// Test_CLIParallelBuildOverride verifies a cli repo can lower build_concurrency
// and raise resource_class when the cold full-matrix cross-compile of a large
// binary OOMs the runner at the "auto"/"large" defaults (observed on
// mcp-kubernetes: a killed build never stores the cache, so the repo stays
// permanently cold). An unset knob keeps its default; a non-cli repo ignores
// both, since the template only renders them inside the ReleaseBinaries block.
func Test_CLIParallelBuildOverride(t *testing.T) {
	cli := render(t, Config{
		RepoName:         repoMCPKubernetes,
		Language:         gen.LanguageGo,
		Flavours:         gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile:    true,
		BuildConcurrency: "2",
		ResourceClass:    resourceClassXLarge,
	})
	// A numeric override must be rendered as a quoted string: the orb's
	// build_concurrency param is string-typed and rejects a bare int.
	if !contains(cli, `build_concurrency: "2"`) {
		t.Errorf("override not applied; want build_concurrency: \"2\" in:\n%s", cli)
	}
	if !contains(cli, "resource_class: "+resourceClassXLarge) {
		t.Errorf("override not applied; want resource_class: xlarge in:\n%s", cli)
	}
	if contains(cli, "build_concurrency: "+DefaultBuildConcurrency) {
		t.Errorf("default build_concurrency leaked through despite override:\n%s", cli)
	}

	// A partial override keeps the unset knob on its default.
	partial := render(t, Config{
		RepoName:         repoMCPKubernetes,
		Language:         gen.LanguageGo,
		Flavours:         gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile:    true,
		BuildConcurrency: "2",
	})
	if !contains(partial, `build_concurrency: "2"`) {
		t.Errorf("partial override not applied; want build_concurrency: \"2\" in:\n%s", partial)
	}
	if !contains(partial, "resource_class: "+DefaultResourceClass) {
		t.Errorf("unset resource_class should default to %q:\n%s", DefaultResourceClass, partial)
	}

	// A non-cli Go service ignores the knobs (no ReleaseBinaries block).
	svc := render(t, Config{
		RepoName:         repoMCPKubernetes,
		Language:         gen.LanguageGo,
		Flavours:         gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile:    true,
		BuildConcurrency: "2",
		ResourceClass:    resourceClassXLarge,
	})
	if contains(svc, "build_concurrency") {
		t.Errorf("non-cli service should not render build_concurrency even when set:\n%s", svc)
	}
	if contains(svc, "resource_class") {
		t.Errorf("non-cli service should not render resource_class even when set:\n%s", svc)
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

// Test_AppVersionFollowsRepoShape verifies which chart jobs keep the appVersion
// declared in Chart.yaml, and that an explicit OverrideChartAppVersion
// overrules the derivation in either direction.
//
// appVersion is the version of the packaged application. A repo that builds its
// own image ships the app it packages, so appVersion is its own version and
// app-build-suite stamps it. A chart-only repo packages an app built elsewhere,
// so the declared appVersion has to survive packaging.
func Test_AppVersionFollowsRepoShape(t *testing.T) {
	yes, no := true, false

	testCases := []struct {
		name     string
		config   Config
		expected int // occurrences of `override_app_version: false`
	}{
		{
			name: "chart-only repo keeps the declared appVersion",
			config: Config{
				RepoName: "agentgateway",
				Language: gen.Language(""),
				Flavours: gen.FlavourSlice{gen.FlavourApp},
			},
			// build-chart and push-chart-release; the branch push job needs BranchPublish.
			expected: 2,
		},
		{
			name: "the branch chart push keeps it too",
			config: Config{
				RepoName:      "agentgateway",
				Language:      gen.Language(""),
				Flavours:      gen.FlavourSlice{gen.FlavourApp},
				BranchPublish: true,
			},
			expected: 3,
		},
		{
			name: "a repo that builds its own image stamps it",
			config: Config{
				RepoName:      "observability-operator",
				Language:      gen.LanguageGo,
				Flavours:      gen.FlavourSlice{gen.FlavourApp},
				HasDockerfile: true,
			},
			expected: 0,
		},
		{
			name: "a nested Dockerfile counts as building the app",
			config: Config{
				RepoName:        "backstage",
				Language:        gen.LanguageNode,
				Flavours:        gen.FlavourSlice{gen.FlavourApp},
				ImageDockerfile: "packages/backend/Dockerfile",
			},
			expected: 0,
		},
		{
			name: "an image-building repo can keep a foreign appVersion",
			config: Config{
				RepoName:                "observability-operator",
				Language:                gen.LanguageGo,
				Flavours:                gen.FlavourSlice{gen.FlavourApp},
				HasDockerfile:           true,
				OverrideChartAppVersion: &no,
			},
			expected: 2,
		},
		{
			name: "a chart-only repo can ask to be stamped anyway",
			config: Config{
				RepoName:                "agentgateway",
				Language:                gen.Language(""),
				Flavours:                gen.FlavourSlice{gen.FlavourApp},
				OverrideChartAppVersion: &yes,
			},
			expected: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, tc.config)
			if n := strings.Count(got, "override_app_version: false"); n != tc.expected {
				t.Errorf("expected override_app_version on %d chart jobs, found %d:\n%s", tc.expected, n, got)
			}
		})
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
		{PackageManagerNPM, "npm ci", "~/.npm", `node-deps-npm-v1-{{ checksum "package-lock.json" }}`, false},
		{PackageManagerYarn, "yarn install --immutable", ".yarn/cache", `node-deps-yarn-v1-{{ checksum "yarn.lock" }}`, false},
		{PackageManagerYarnClassic, "yarn install --frozen-lockfile", "~/.cache/yarn", `node-deps-yarn-classic-v1-{{ checksum "yarn.lock" }}`, false},
		{PackageManagerPNPM, "pnpm install --frozen-lockfile", "~/.local/share/pnpm/store", `node-deps-pnpm-v1-{{ checksum "pnpm-lock.yaml" }}`, true},
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

// Test_ATSInputsForAppRepo verifies the canonical ATS test dependencies are
// emitted for a chart/app (.HasApp) repo -- the same signal that gates
// run-tests-with-ats -- in the uv layout app-test-suite 1.x (the default tag)
// consumes: tests/ats/pyproject.toml + uv.lock with the centrally-pinned
// content, and the pipenv files the generator used to emit deleted.
func Test_ATSInputsForAppRepo(t *testing.T) {
	inputs := newCircleCI(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	}).ATSInputs()

	if len(inputs) != 4 {
		t.Fatalf("expected 4 ATS inputs for an app repo (uv layout), got %d: %+v", len(inputs), inputs)
	}
	if inputs[0].Path != "tests/ats/pyproject.toml" {
		t.Errorf("ATS input Path = %q, want tests/ats/pyproject.toml", inputs[0].Path)
	}

	got := renderInput(t, inputs[0])
	if !contains(got, `"pytest-helm-charts==1.3.4"`) {
		t.Errorf("generated ATS pyproject.toml missing the canonical pytest-helm-charts pin:\n%s", got)
	}
	if !contains(got, `"pytest==8.4.2"`) {
		t.Errorf("generated ATS pyproject.toml missing the canonical pytest pin:\n%s", got)
	}
}

// Test_ATSPipfileOmittedForNonApp verifies a repo without the app flavour gets
// no ATS Pipfile -- the file is scoped to chart/app repos exactly like the
// run-tests-with-ats jobs.
func Test_ATSPipfileOmittedForNonApp(t *testing.T) {
	inputs := newCircleCI(t, Config{
		RepoName:      "klausctl",
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{},
		HasDockerfile: false,
	}).ATSInputs()

	if len(inputs) != 0 {
		t.Errorf("expected no ATS inputs for a non-app repo, got %d: %+v", len(inputs), inputs)
	}
}

// Test_SkipATSOmitsChartTests verifies the ATS opt-out: an app repo with
// SkipATS gets the chart pipeline but no run-tests-with-ats jobs, and the
// chart push jobs gate directly on build-chart instead of the test jobs. The
// canonical Pipfile is suppressed too.
func Test_SkipATSOmitsChartTests(t *testing.T) {
	c := Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		BranchPublish: true,
		SkipATS:       true,
	}

	got := render(t, c)

	// The chart pipeline itself stays.
	for _, want := range []string{"name: build-chart", "name: push-chart", "name: push-chart-release"} {
		if !contains(got, want) {
			t.Errorf("SkipATS config missing %q:\n%s", want, got)
		}
	}
	// The ATS test jobs are gone.
	for _, unwanted := range []string{jobRunTests, "execute-chart-tests", "execute-chart-tests-release"} {
		if contains(got, unwanted) {
			t.Errorf("SkipATS config should not contain %q:\n%s", unwanted, got)
		}
	}
	// The chart push jobs gate on build-chart now that the test jobs are gone.
	if !contains(got, "requires:\n        - build-chart") {
		t.Errorf("SkipATS chart push should require build-chart directly:\n%s", got)
	}

	// No canonical ATS Pipfile is emitted.
	if inputs := newCircleCI(t, c).ATSInputs(); len(inputs) != 0 {
		t.Errorf("expected no ATS inputs with SkipATS, got %d: %+v", len(inputs), inputs)
	}
}

// Test_DefaultChartTestsRunOnBranchesOnly verifies the default chart-test
// shape: an app repo gets execute-chart-tests on branches and the canonical
// Pipfile, no execute-chart-tests-release, and push-chart-release gates
// directly on build-chart (plus the release image).
func Test_DefaultChartTestsRunOnBranchesOnly(t *testing.T) {
	c := Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	}

	got := render(t, c)

	for _, want := range []string{"name: build-chart", "name: execute-chart-tests\n", "name: push-chart-release"} {
		if !contains(got, want) {
			t.Errorf("default config missing %q:\n%s", want, got)
		}
	}
	// Exactly one run-tests-with-ats job: no tag-time re-run by default.
	if contains(got, "execute-chart-tests-release") {
		t.Errorf("default config should not contain execute-chart-tests-release:\n%s", got)
	}
	if n := strings.Count(got, jobRunTests); n != 1 {
		t.Errorf("default config should carry exactly one %s job, found %d:\n%s", jobRunTests, n, got)
	}
	// push-chart-release gates on build-chart and the release image, and is
	// the only job with that requires block.
	if n := strings.Count(got, "requires:\n        - build-chart\n        - push-to-registries-release"); n != 1 {
		t.Errorf("default chart push should require build-chart + push-to-registries-release exactly once, found %d:\n%s", n, got)
	}

	// The canonical ATS test dependencies stay (uv layout: pyproject.toml,
	// uv.lock, the two Pipfile deletes): the branch job runs the tests.
	if inputs := newCircleCI(t, c).ATSInputs(); len(inputs) != 4 {
		t.Errorf("expected 4 ATS inputs by default, got %d: %+v", len(inputs), inputs)
	}
}

// Test_ATSOnReleaseAddsTagRun verifies the opt-in back to the pre-v8.45.0
// shape: ATSOnRelease adds execute-chart-tests-release on the tag, after the
// release image, and push-chart-release gates on it. SkipATS and ATSOnRelease
// together are rejected.
func Test_ATSOnReleaseAddsTagRun(t *testing.T) {
	c := Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		ATSOnRelease:  true,
	}

	got := render(t, c)

	if n := strings.Count(got, jobRunTests); n != 2 {
		t.Errorf("ATSOnRelease config should carry two %s jobs, found %d:\n%s", jobRunTests, n, got)
	}
	// The tag-time job carries the ATS 1.x parameters between its name and its
	// requires block, so locate the job and check its requires separately.
	tagJob := got[strings.Index(got, "name: execute-chart-tests-release\n"):]
	if next := strings.Index(tagJob, "\n    - "); next > 0 {
		tagJob = tagJob[:next]
	}
	if !contains(tagJob, "requires:\n        - build-chart\n        - push-to-registries-release") {
		t.Errorf("ATSOnRelease tag test should require build-chart + push-to-registries-release:\n%s", got)
	}
	if !contains(got, "requires:\n        - execute-chart-tests-release\n        - push-to-registries-release") {
		t.Errorf("ATSOnRelease chart push should require execute-chart-tests-release:\n%s", got)
	}
	if inputs := newCircleCI(t, c).ATSInputs(); len(inputs) != 4 {
		t.Errorf("expected 4 ATS inputs with ATSOnRelease, got %d: %+v", len(inputs), inputs)
	}

	c.SkipATS = true
	if _, err := New(c); !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for SkipATS + ATSOnRelease, got %v", err)
	}
}

// Test_GoldenChartOnlyWorkflows pins the chart-only default shape: the
// agent-platform-standalone case (generic language, app flavour, no
// Dockerfile, appVersion stamped), chart tests on branches only.
func Test_GoldenChartOnlyWorkflows(t *testing.T) {
	stamp := true
	got := render(t, Config{
		RepoName:                repoAPStandalone,
		Language:                gen.LanguageGeneric,
		Flavours:                gen.FlavourSlice{gen.FlavourApp},
		OverrideChartAppVersion: &stamp,
	})

	want, err := os.ReadFile(goldenChartOnlyWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenChartOnlyWorkflowsPath, got, string(want))
	}
}

// Test_GoldenATSOnReleaseWorkflows pins the opt-in tag-time chart-test shape
// for the Go service: the pre-v8.45.0 default, byte for byte.
func Test_GoldenATSOnReleaseWorkflows(t *testing.T) {
	got := render(t, Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		ATSOnRelease:  true,
	})

	want, err := os.ReadFile(goldenATSOnReleaseWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenATSOnReleaseWorkflowsPath, got, string(want))
	}
}

// Test_NodeBuildOutputCache verifies the build-output cache (node_modules +
// Yarn install-state) is emitted for the Yarn package managers, keyed on the
// node image version, and is absent for npm (npm ci wipes node_modules) and
// pnpm (its store already caches build side-effects).
func Test_NodeBuildOutputCache(t *testing.T) {
	berryKey := "node-build-yarn-v1-" + DefaultNodeImageVersion + `-{{ checksum "yarn.lock" }}`
	classicKey := "node-build-yarn-classic-v1-" + DefaultNodeImageVersion + `-{{ checksum "yarn.lock" }}`

	berry := render(t, Config{RepoName: repoK8sTypes, Language: gen.LanguageNode, PackageManager: PackageManagerYarn})
	if !contains(berry, berryKey) {
		t.Errorf("yarn-berry should emit the build cache key %q:\n%s", berryKey, berry)
	}
	if !contains(berry, "- node_modules") || !contains(berry, "- .yarn/install-state.gz") {
		t.Errorf("yarn-berry build cache should save node_modules + install-state:\n%s", berry)
	}

	classic := render(t, Config{RepoName: repoK8sTypes, Language: gen.LanguageNode, PackageManager: PackageManagerYarnClassic})
	if !contains(classic, classicKey) {
		t.Errorf("yarn-classic should emit the build cache key %q:\n%s", classicKey, classic)
	}
	if contains(classic, "- .yarn/install-state.gz") {
		t.Errorf("yarn-classic build cache should not reference Berry install-state:\n%s", classic)
	}

	for _, pm := range []string{PackageManagerNPM, PackageManagerPNPM} {
		got := render(t, Config{RepoName: repoK8sTypes, Language: gen.LanguageNode, PackageManager: pm})
		if contains(got, "node-build-"+pm) || contains(got, "- node_modules") {
			t.Errorf("%s should not emit a build-output cache:\n%s", pm, got)
		}
	}
}

// Test_NodeResourceClass verifies the Node job renders the default resource
// class when unset and honours the gen.ci.resourceClass override (the same knob
// the cli go-build job uses), so a memory-hungry monorepo verify/build can be
// sized up per repo without forking the generated job.
func Test_NodeResourceClass(t *testing.T) {
	def := render(t, Config{
		RepoName:       repoK8sTypes,
		Language:       gen.LanguageNode,
		PackageManager: PackageManagerNPM,
	})
	if !contains(def, "resource_class: "+DefaultNodeResourceClass) {
		t.Errorf("Node job should default resource_class to %q:\n%s", DefaultNodeResourceClass, def)
	}

	override := render(t, Config{
		RepoName:       repoK8sTypes,
		Language:       gen.LanguageNode,
		PackageManager: PackageManagerNPM,
		ResourceClass:  resourceClassXLarge,
	})
	if !contains(override, "resource_class: "+resourceClassXLarge) {
		t.Errorf("Node job resource_class override not applied:\n%s", override)
	}
	if contains(override, "resource_class: "+DefaultNodeResourceClass) {
		t.Errorf("default resource_class leaked through despite override:\n%s", override)
	}
}

// Test_NodeImageVersion verifies the Node job renders devctl's baked-in default
// when a repo pins nothing, and honours a repo's own pin (detected from .nvmrc
// by the runner) otherwise. The pin must reach the executor image AND the
// node-build cache-key salt together: those two are the pair that must never
// disagree, since the cached node_modules holds native addons whose ABI is tied
// to the node version. Bumping only one is exactly the failure a per-file
// dependency bot causes when it edits the rendered image line alone.
func Test_NodeImageVersion(t *testing.T) {
	// Deliberately a version that can never become the real default, so the
	// leak assertion below stays meaningful. A plausible next version (24.19.0)
	// would silently pass once DefaultNodeImageVersion caught up to it, and
	// then fail with a message describing the opposite of what happened.
	const pinned = "99.0.0"

	def := render(t, Config{
		RepoName:       repoK8sTypes,
		Language:       gen.LanguageNode,
		PackageManager: PackageManagerYarn,
	})
	if !contains(def, "image: cimg/node:"+DefaultNodeImageVersion) {
		t.Errorf("Node job should default the image to %q:\n%s", DefaultNodeImageVersion, def)
	}

	override := render(t, Config{
		RepoName:         repoK8sTypes,
		Language:         gen.LanguageNode,
		PackageManager:   PackageManagerYarn,
		NodeImageVersion: pinned,
	})
	for _, want := range []string{
		"image: cimg/node:" + pinned,
		"node-build-yarn-v1-" + pinned + "-",
	} {
		if !contains(override, want) {
			t.Errorf("Node image version override should render %q:\n%s", want, override)
		}
	}
	if contains(override, DefaultNodeImageVersion) {
		t.Errorf("default node image version leaked through despite override:\n%s", override)
	}
}

// Test_NodeBuildCacheSavedAfterBuild verifies the build-output cache is saved
// after the verify/build steps (not right after install), so it captures the
// tsc/eslint/jest incremental caches under node_modules/.cache in addition to
// the native addons compiled during install -- the compute-side win.
func Test_NodeBuildCacheSavedAfterBuild(t *testing.T) {
	got := render(t, Config{
		RepoName:        repoBackstage,
		Language:        gen.LanguageNode,
		Flavours:        gen.FlavourSlice{gen.FlavourApp},
		PackageManager:  PackageManagerYarn,
		NodeBuildTarget: nodeBuildTarget,
		NodeBuildOutput: backstageBuildOutput,
		ImageDockerfile: backstageDockerfile,
	})

	buildSave := strings.Index(got, "key: node-build-yarn-v1-")
	verify := strings.Index(got, "name: Verify")
	build := strings.Index(got, "name: Build")
	if buildSave < 0 || verify < 0 || build < 0 {
		t.Fatalf("missing expected steps (buildSave=%d verify=%d build=%d):\n%s", buildSave, verify, build, got)
	}
	if buildSave < verify || buildSave < build {
		t.Errorf("build-output save_cache must come after Verify and Build to capture compute caches; got save=%d verify=%d build=%d:\n%s", buildSave, verify, build, got)
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

// Test_GoBuildPath verifies the go-build job compiles the configured package
// for repos whose main lives under a different path.
func Test_GoBuildPath(t *testing.T) {
	got := render(t, Config{
		RepoName:      "coredns-warnlist-plugin",
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourGeneric},
		HasDockerfile: true,
		GoBuildPath:   "./cmd/coredns",
	})
	if n := strings.Count(got, "        path: ./cmd/coredns\n"); n != 1 {
		t.Errorf("expected exactly one go-build path param, found %d:\n%s", n, got)
	}

	def := render(t, Config{
		RepoName:      "coredns-warnlist-plugin",
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourGeneric},
		HasDockerfile: true,
	})
	if contains(def, "\n        path:") {
		t.Errorf("no path param should be emitted without GoBuildPath (orb default applies):\n%s", def)
	}
}

// Test_GoBuildPathRequiresGo verifies the knob is rejected for a repo that
// renders no go-build job, so a misplaced setting fails at generation time
// instead of silently doing nothing.
func Test_GoBuildPathRequiresGo(t *testing.T) {
	_, err := New(Config{
		RepoName:      "coredns-warnlist-plugin",
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		GoBuildPath:   "./cmd/coredns",
	})
	if !IsInvalidConfig(err) {
		t.Fatalf("expected invalidConfigError, got %v", err)
	}
}

// Test_GoTestArtifacts verifies the go-build job gains post-steps that keep
// the configured directory as a build artifact on failure, that the path is
// normalized, and that nothing else changes: the same config without the knob
// renders no post-steps at all and the job list is identical.
func Test_GoTestArtifacts(t *testing.T) {
	base := Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile: true,
	}
	def := render(t, base)
	if contains(def, "post-steps:") || contains(def, "store_artifacts") {
		t.Errorf("no post-steps should be emitted without GoTestArtifacts:\n%s", def)
	}

	withKnob := base
	withKnob.GoTestArtifacts = "test-reports/"
	got := render(t, withKnob)
	for _, want := range []string{
		"        post-steps:\n",
		"            name: Collect test artifacts\n",
		"              mkdir -p /tmp/go-test-artifacts\n",
		"              cp -r test-reports/. /tmp/go-test-artifacts/ 2>/dev/null || true\n",
		"            when: on_fail\n",
		"        - store_artifacts:\n            path: /tmp/go-test-artifacts\n            destination: test-reports\n",
	} {
		if n := strings.Count(got, want); n != 1 {
			t.Errorf("expected exactly one %q, found %d:\n%s", want, n, got)
		}
	}
	if contains(got, "test-reports//") {
		t.Errorf("trailing slash must be normalized away:\n%s", got)
	}
	// Confined to the go-build job: workflow job entries sit at a 4-space
	// indent, the post-steps entries at 8, so the job count must not move.
	if strings.Count(got, "\n    - ") != strings.Count(def, "\n    - ") {
		t.Errorf("GoTestArtifacts must not add or remove workflow jobs:\n%s", got)
	}
}

// Test_GoldenGoTestArtifacts pins the whole rendering for muster's shape (Go
// service with a chart and cli binaries, the repo that motivated the knob:
// its integration suite writes one JSON per scenario to test-reports/).
func Test_GoldenGoTestArtifacts(t *testing.T) {
	got := render(t, Config{
		RepoName:        "muster",
		Language:        gen.LanguageGo,
		Flavours:        gen.FlavourSlice{gen.FlavourGeneric, gen.FlavourApp, gen.FlavourCLI},
		HasDockerfile:   true,
		GoTestArtifacts: "test-reports",
	})

	want, err := os.ReadFile(goldenGoTestArtifactsWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenGoTestArtifactsWorkflowsPath, got, string(want))
	}
}

// Test_GoTestArtifactsRejects verifies the knob fails at generation time for a
// repo that renders no go-build job and for paths the generated post-steps
// cannot stage safely (the value is spliced into a shell command unquoted).
func Test_GoTestArtifactsRejects(t *testing.T) {
	goRepo := func(dir string) Config {
		return Config{
			RepoName:        repoMCPKubernetes,
			Language:        gen.LanguageGo,
			Flavours:        gen.FlavourSlice{gen.FlavourGeneric},
			HasDockerfile:   true,
			GoTestArtifacts: dir,
		}
	}
	cases := map[string]Config{
		"non-go repo": {
			RepoName:        "agent-platform-standalone",
			Flavours:        gen.FlavourSlice{gen.FlavourApp},
			GoTestArtifacts: "test-reports",
		},
		"absolute path":        goRepo("/tmp/test-reports"),
		"escapes the checkout": goRepo("../test-reports"),
		"checkout root":        goRepo("."),
		"shell metacharacters": goRepo("test-reports; rm -rf /"),
		"whitespace":           goRepo("test reports"),
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := New(c)
			if !IsInvalidConfig(err) {
				t.Fatalf("expected invalidConfigError, got %v", err)
			}
		})
	}
}

// nativeNodeConfig is the backstage shape with the native per-architecture
// image build opted in: a Node monorepo whose Dockerfile does real work in RUN
// steps, publishing a dev image on branches (BranchPublish), so both the branch
// and the release path get the build-image jobs plus a merge.
func nativeNodeConfig() Config {
	return Config{
		RepoName:          repoBackstage,
		Language:          gen.LanguageNode,
		Flavours:          gen.FlavourSlice{gen.FlavourApp},
		PackageManager:    PackageManagerYarn,
		NodeBuildTarget:   nodeBuildTarget,
		NodeBuildOutput:   backstageBuildOutput,
		ImageDockerfile:   backstageDockerfile,
		ImagePlatforms:    "linux/amd64,linux/arm64",
		BranchPublish:     true,
		ImageNativeBuilds: true,
	}
}

// Test_ImageNativeBuilds verifies the opt-in per-architecture image build: one
// architect/build-image job per platform on a class of that architecture, and
// the push-to-registries jobs switched to merge-digests with a platform list
// that matches the build jobs -- on both the branch (BranchPublish) and the
// release path. Off, the output is the single-job shape, which the other
// goldens pin.
func Test_ImageNativeBuilds(t *testing.T) {
	got := render(t, nativeNodeConfig())

	for _, want := range []string{
		"name: build-image-amd64\n        platform: linux/amd64\n        resource_class: small",
		"name: build-image-arm64\n        platform: linux/arm64\n        resource_class: arm.medium",
		"name: build-image-release-amd64\n        platform: linux/amd64\n        resource_class: small",
		"name: build-image-release-arm64\n        platform: linux/arm64\n        resource_class: arm.medium",
		"name: push-to-registries\n        merge-digests: true\n        platforms: \"linux/amd64,linux/arm64\"",
		"name: push-to-registries-release\n        merge-digests: true\n        platforms: \"linux/amd64,linux/arm64\"",
		"requires:\n        - build-image-amd64\n        - build-image-arm64",
		"requires:\n        - build-image-release-amd64\n        - build-image-release-arm64",
	} {
		if !contains(got, want) {
			t.Errorf("native builds output missing %q:\n%s", want, got)
		}
	}

	// The Dockerfile path belongs to the four build jobs; the merge jobs do not
	// build and must not carry it. split-china-push has to be identical on the
	// release build jobs and the release merge job (three in total).
	if n := strings.Count(got, "dockerfile: "+backstageDockerfile); n != 4 {
		t.Errorf("expected dockerfile on the four build-image jobs, found %d:\n%s", n, got)
	}
	if n := strings.Count(got, "split-china-push: true"); n != 3 {
		t.Errorf("expected split-china-push on two release builds + the merge, found %d:\n%s", n, got)
	}
	// Nothing is emulated: the single multi-platform push job must be gone.
	if contains(got, "platforms: linux/amd64,linux/arm64\n") {
		t.Errorf("unquoted single-job platforms param leaked into the native shape:\n%s", got)
	}
	// The downstream edges keep resolving by the unchanged job names.
	for _, want := range []string{
		"- execute-chart-tests\n        - push-to-registries\n",
		"- build-chart\n        - push-to-registries-release\n",
	} {
		if !contains(got, want) {
			t.Errorf("downstream requires lost the push-to-registries edge %q:\n%s", want, got)
		}
	}
}

// Test_ImageNativeBuildsValidateOnlyBranch verifies the branch path without
// BranchPublish: the per-architecture builds run with push: false and there
// is no branch push-to-registries job, since nothing was pushed to merge. This
// is the Go service shape (mcp-kubernetes).
func Test_ImageNativeBuildsValidateOnlyBranch(t *testing.T) {
	got := render(t, Config{
		RepoName:          repoMCPKubernetes,
		Language:          gen.LanguageGo,
		Flavours:          gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile:     true,
		ImageNativeBuilds: true,
	})

	if n := strings.Count(got, "push: false"); n != 2 {
		t.Errorf("expected push: false on the two branch build-image jobs, found %d:\n%s", n, got)
	}
	if contains(got, "name: push-to-registries\n") {
		t.Errorf("validate-only branch path must not emit a branch push-to-registries job:\n%s", got)
	}
	if !contains(got, "name: push-to-registries-release\n        merge-digests: true") {
		t.Errorf("release path must merge digests:\n%s", got)
	}
	// Empty ImagePlatforms resolves to the default pair, and the merge job must
	// name it explicitly: the orb needs the set to match the build jobs.
	if !contains(got, "platforms: \"linux/amd64,linux/arm64\"") {
		t.Errorf("default platform list not resolved onto the merge job:\n%s", got)
	}
	if !contains(got, "name: build-image\n") == false {
		t.Errorf("single-job branch validation job must not appear next to the native builds:\n%s", got)
	}
}

// Test_ImageNativeBuildsRejectsUnmappedPlatform verifies a platform with no
// native CircleCI class is refused at generation time when native builds are
// on -- the orb has no emulated fallback -- and passes through untouched when
// they are off, where the single-job buildx build emulates it as before.
func Test_ImageNativeBuildsRejectsUnmappedPlatform(t *testing.T) {
	_, err := New(Config{
		RepoName:          "vllm",
		Language:          gen.Language(""),
		Flavours:          gen.FlavourSlice{},
		HasDockerfile:     true,
		ImagePlatforms:    "linux/arm64,linux/arm/v7",
		ImageNativeBuilds: true,
	})
	if !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for an unmapped platform with native builds, got %v", err)
	}

	got := render(t, Config{
		RepoName:       "vllm",
		Language:       gen.Language(""),
		Flavours:       gen.FlavourSlice{},
		HasDockerfile:  true,
		ImagePlatforms: "linux/arm64,linux/arm/v7",
	})
	if n := strings.Count(got, "platforms: linux/arm64,linux/arm/v7"); n != 2 {
		t.Errorf("single-job build must pass the platform list through, found %d:\n%s", n, got)
	}
}

// Test_GoldenNativeWorkflows pins the native per-architecture shape for a Go
// service (validate-only branch path, merged release path).
func Test_GoldenNativeWorkflows(t *testing.T) {
	got := render(t, Config{
		RepoName:          repoMCPKubernetes,
		Language:          gen.LanguageGo,
		Flavours:          gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile:     true,
		ImageNativeBuilds: true,
	})

	want, err := os.ReadFile(goldenNativeWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenNativeWorkflowsPath, got, string(want))
	}
}

// Test_GoldenNativeNodeWorkflows pins the native per-architecture shape for the
// backstage case: Node monorepo, nested Dockerfile, BranchPublish.
func Test_GoldenNativeNodeWorkflows(t *testing.T) {
	got := render(t, nativeNodeConfig())

	want, err := os.ReadFile(goldenNativeNodeWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenNativeNodeWorkflowsPath, got, string(want))
	}
}

// Test_ATSVersionOnePointX verifies a 1.x app-test-suite tag pins the image on
// the chart-test job, turns on the job-owned kind cluster (app-test-suite 1.x
// provisions none), and switches the generated test dependencies to the uv
// layout, deleting the pipenv files the generator used to emit.
func Test_ATSVersionOnePointX(t *testing.T) {
	c := Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
		ATSVersion:    "1.0.0",
	}

	got := render(t, c)
	if n := strings.Count(got, `app-test-suite_container_tag: "1.0.0"`); n != 1 {
		t.Errorf("expected the ATS tag on the chart-test job, found %d:\n%s", n, got)
	}
	if n := strings.Count(got, "create_kind_cluster: true"); n != 1 {
		t.Errorf("expected create_kind_cluster on the chart-test job, found %d:\n%s", n, got)
	}

	inputs := newCircleCI(t, c).ATSInputs()
	want := []struct {
		path   string
		delete bool
	}{
		{"tests/ats/pyproject.toml", false},
		{"tests/ats/uv.lock", false},
		{"tests/ats/Pipfile", true},
		{"tests/ats/Pipfile.lock", true},
	}
	if len(inputs) != len(want) {
		t.Fatalf("expected %d ATS inputs for the uv layout, got %d: %+v", len(want), len(inputs), inputs)
	}
	for i, w := range want {
		if inputs[i].Path != w.path || inputs[i].Delete != w.delete {
			t.Errorf("ATS input %d = {Path: %q, Delete: %v}, want {Path: %q, Delete: %v}", i, inputs[i].Path, inputs[i].Delete, w.path, w.delete)
		}
	}
	if pyproject := renderInput(t, inputs[0]); !contains(pyproject, `"pytest-helm-charts==1.3.4"`) {
		t.Errorf("generated pyproject.toml missing the canonical pytest-helm-charts pin:\n%s", pyproject)
	}
}

// Test_ATSVersionLegacy verifies a 0.x tag (a release or a dev build of the
// pre-1.0 tool) only pins the image: the dats.sh path and the Pipfile stay.
func Test_ATSVersionLegacy(t *testing.T) {
	for _, tag := range []string{"0.15.0", "v0.15.0", "0.15.1-dev.gh-readonl--ab3270cae7f.2026-08-20.21-58-02.h4162ff7"} {
		c := Config{
			RepoName:      repoMCPKubernetes,
			Language:      gen.LanguageGo,
			Flavours:      gen.FlavourSlice{gen.FlavourApp},
			HasDockerfile: true,
			ATSVersion:    tag,
		}

		got := render(t, c)
		if n := strings.Count(got, `app-test-suite_container_tag: "`+tag+`"`); n != 1 {
			t.Errorf("%s: expected the ATS tag on the chart-test job, found %d:\n%s", tag, n, got)
		}
		if contains(got, "create_kind_cluster") {
			t.Errorf("%s: a 0.x tag must not turn on create_kind_cluster:\n%s", tag, got)
		}
		if inputs := newCircleCI(t, c).ATSInputs(); len(inputs) != 1 || inputs[0].Path != "tests/ats/Pipfile" {
			t.Errorf("%s: expected the Pipfile as the only ATS input, got %+v", tag, inputs)
		}
	}
}

// Test_ATSVersionUnset verifies an unset tag selects DefaultATSVersion: the
// generated config is byte-identical to pinning the default explicitly, and
// that default is app-test-suite 1.x (pinned image, job-owned kind cluster), so
// every generated-CI chart repo runs ATS 1.x unless it pins a 0.x tag.
func Test_ATSVersionUnset(t *testing.T) {
	base := Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	}
	got := render(t, base)

	explicit := base
	explicit.ATSVersion = DefaultATSVersion
	if want := render(t, explicit); got != want {
		t.Errorf("unset ATSVersion must render like ATSVersion=%q\n--- unset ---\n%s\n--- explicit ---\n%s", DefaultATSVersion, got, want)
	}
	if !strings.HasPrefix(DefaultATSVersion, "1.") {
		t.Errorf("DefaultATSVersion = %q, want an app-test-suite 1.x tag", DefaultATSVersion)
	}
	for _, want := range []string{`app-test-suite_container_tag: "` + DefaultATSVersion + `"`, "create_kind_cluster: true"} {
		if !contains(got, want) {
			t.Errorf("default config must contain %q:\n%s", want, got)
		}
	}
}

// Test_ATSVersionInvalid verifies the tag has to be a semantic version, and
// that the ATS opt-out simply wins over the tag: SkipATS carries the default
// (or an explicit) tag without error and still emits no chart-test job and no
// test dependency file.
func Test_ATSVersionInvalid(t *testing.T) {
	base := Config{
		RepoName:      repoMCPKubernetes,
		Language:      gen.LanguageGo,
		Flavours:      gen.FlavourSlice{gen.FlavourApp},
		HasDockerfile: true,
	}

	c := base
	c.ATSVersion = "latest"
	if _, err := New(c); !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for a non-semver ATS tag, got %v", err)
	}

	c = base
	c.ATSVersion = "1.0.0"
	c.SkipATS = true
	got := render(t, c)
	if contains(got, jobRunTests) || contains(got, "create_kind_cluster") {
		t.Errorf("SkipATS must drop the chart-test jobs regardless of ATSVersion:\n%s", got)
	}
	if inputs := newCircleCI(t, c).ATSInputs(); len(inputs) != 0 {
		t.Errorf("SkipATS must emit no ATS inputs regardless of ATSVersion, got %+v", inputs)
	}
}

// Test_GoldenATSOnePointXWorkflows pins the chart-only shape on app-test-suite
// 1.x: the agent-platform-standalone case (generic language, app flavour, no
// Dockerfile, appVersion stamped, branch-only chart tests) with the pinned
// image and the job-owned kind cluster.
func Test_GoldenATSOnePointXWorkflows(t *testing.T) {
	stamp := true
	got := render(t, Config{
		RepoName:                repoAPStandalone,
		Language:                gen.LanguageGeneric,
		Flavours:                gen.FlavourSlice{gen.FlavourApp},
		OverrideChartAppVersion: &stamp,
		ATSVersion:              "1.0.0",
	})

	want, err := os.ReadFile(goldenATSOnePointXWorkflowsPath) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("generated workflows do not match golden %s\n--- got ---\n%s\n--- want ---\n%s", goldenATSOnePointXWorkflowsPath, got, string(want))
	}
}

// Test_ImageResourceClasses verifies the per-platform class override lands on
// both legs (branch and release) of that platform only, and that the other
// platform keeps its default class.
func Test_ImageResourceClasses(t *testing.T) {
	got := render(t, Config{
		RepoName:             "vllm",
		Language:             gen.Language(""),
		Flavours:             gen.FlavourSlice{},
		HasDockerfile:        true,
		ImageNativeBuilds:    true,
		ImageResourceClasses: map[string]string{"linux/arm64": "arm.large"},
	})
	if n := strings.Count(got, "resource_class: arm.large"); n != 2 {
		t.Errorf("expected arm.large on the branch and release arm64 legs, found %d:\n%s", n, got)
	}
	if contains(got, "resource_class: arm.medium") {
		t.Errorf("the arm64 default class must be replaced, not added to:\n%s", got)
	}
	if n := strings.Count(got, "resource_class: small"); n != 2 {
		t.Errorf("the amd64 legs must keep their default class, found %d:\n%s", n, got)
	}

	// Single-platform image: the override applies to the only leg pair and no
	// amd64 job is emitted.
	single := render(t, Config{
		RepoName:             "vllm",
		Language:             gen.Language(""),
		Flavours:             gen.FlavourSlice{},
		HasDockerfile:        true,
		ImagePlatforms:       "linux/arm64",
		ImageNativeBuilds:    true,
		ImageResourceClasses: map[string]string{"linux/arm64": "arm.large"},
	})
	if n := strings.Count(single, "resource_class: arm.large"); n != 2 {
		t.Errorf("expected arm.large on both arm64 legs of the single-platform image, found %d:\n%s", n, single)
	}
	if contains(single, "linux/amd64") {
		t.Errorf("no amd64 job for a single-platform image:\n%s", single)
	}
}

// Test_ImageResourceClassesRejects verifies the override is refused at
// generation time when the class is not one of the platform's architecture
// (the orb has no emulated fallback), when it names a platform the image does
// not build, and when native builds are off (the single job runs on the orb
// default class, so the override would be silently ignored).
func Test_ImageResourceClassesRejects(t *testing.T) {
	base := func() Config {
		return Config{
			RepoName:          "vllm",
			Language:          gen.Language(""),
			Flavours:          gen.FlavourSlice{},
			HasDockerfile:     true,
			ImageNativeBuilds: true,
		}
	}

	wrongArch := base()
	wrongArch.ImageResourceClasses = map[string]string{"linux/arm64": "large"}
	if _, err := New(wrongArch); !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for an x86 class on linux/arm64, got %v", err)
	}

	unknownClass := base()
	unknownClass.ImageResourceClasses = map[string]string{"linux/amd64": "arm.large"}
	if _, err := New(unknownClass); !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for an Arm class on linux/amd64, got %v", err)
	}

	notBuilt := base()
	notBuilt.ImagePlatforms = "linux/arm64"
	notBuilt.ImageResourceClasses = map[string]string{"linux/amd64": "large"}
	if _, err := New(notBuilt); !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for an override on a platform that is not built, got %v", err)
	}

	noNative := base()
	noNative.ImageNativeBuilds = false
	noNative.ImageResourceClasses = map[string]string{"linux/arm64": "arm.large"}
	if _, err := New(noNative); !IsInvalidConfig(err) {
		t.Errorf("expected invalidConfigError for ImageResourceClasses without ImageNativeBuilds, got %v", err)
	}
}
