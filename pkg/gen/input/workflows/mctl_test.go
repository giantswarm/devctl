package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/file"
)

// mctlNames are the generated names, zz_generated.mctl_<name>.yaml.
var mctlNames = []string{
	"close_cascade",
	"config",
	"create_mc",
	"init",
	"merge_cascade",
	"merge_gate",
	"patch_init",
	"validate_config",
	"validate_manifest",
}

const templateRepoGuard = "github.repository != 'giantswarm/template-management-clusters'"

type mctlWorkflow struct {
	Env  map[string]string `yaml:"env"`
	Jobs map[string]struct {
		If    string `yaml:"if"`
		Steps []struct {
			Name string            `yaml:"name"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// mctlRenders renders every mctl workflow of a fleet repository, by name.
func mctlRenders(t *testing.T, fixedHeader bool) map[string]string {
	t.Helper()

	inputs := newWorkflows(t, gen.FlavourManagementClustersFleet).Mctl()
	if len(inputs) != len(mctlNames) {
		t.Fatalf("Mctl() returned %d inputs, want %d", len(inputs), len(mctlNames))
	}

	got := map[string]string{}
	for i, in := range inputs {
		if fixedHeader {
			in = withFixedHeader(t, in)
		}
		got[mctlNames[i]] = renderInput(t, in)
	}
	return got
}

func parseMctl(t *testing.T, name, body string) mctlWorkflow {
	t.Helper()

	var wf mctlWorkflow
	if err := yaml.Unmarshal([]byte(body), &wf); err != nil {
		t.Fatalf("%s is not valid YAML: %v\n%s", name, err, body)
	}
	return wf
}

func mctlStepScript(t *testing.T, name, step string) string {
	t.Helper()

	wf := parseMctl(t, name, mctlRenders(t, false)[name])
	for _, job := range wf.Jobs {
		for _, s := range job.Steps {
			if s.Name == step {
				return s.Run
			}
		}
	}
	t.Fatalf("step %q not found in %s", step, name)
	return ""
}

// Test_MctlPaths pins the generated and the deleted legacy file names.
func Test_MctlPaths(t *testing.T) {
	w := newWorkflows(t, gen.FlavourManagementClustersFleet)

	var got []string
	for _, in := range w.Mctl() {
		got = append(got, in.Path)
	}
	var want []string
	for _, name := range mctlNames {
		want = append(want, filepath.Join(".github", "workflows", "zz_generated.mctl_"+name+".yaml"))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("paths =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	var deleted []string
	for _, in := range w.MctlLegacyDeletion() {
		if !in.Delete {
			t.Errorf("%s is not a deletion", in.Path)
		}
		deleted = append(deleted, filepath.Base(in.Path))
	}
	wantDeleted := "close-cascade.yaml config.yaml create-mc.yaml init.yaml merge-cascade.yaml merge-gate.yaml patch-init.yaml validate-config.yaml validate-manifest.yaml op.env"
	if strings.Join(deleted, " ") != wantDeleted {
		t.Errorf("deleted = %v, want %s", deleted, wantDeleted)
	}
}

// Test_GoldenMctl pins the rendered workflows; they are repository-independent.
func Test_GoldenMctl(t *testing.T) {
	for name, got := range mctlRenders(t, true) {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, filepath.Join("testdata", "mctl_"+name+".yaml.golden"), got)
		})
	}
}

// Test_MctlGuardsTemplateRepo checks every job's if starts with the template repo guard and has no top-level ||.
func Test_MctlGuardsTemplateRepo(t *testing.T) {
	for name, body := range mctlRenders(t, false) {
		wf := parseMctl(t, name, body)
		if len(wf.Jobs) == 0 {
			t.Errorf("%s has no jobs", name)
		}
		for job, j := range wf.Jobs {
			cond := strings.TrimSpace(j.If)
			if !strings.HasPrefix(cond, templateRepoGuard) {
				t.Errorf("%s job %s if = %q, want it to start with the template repo guard", name, job, cond)
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(cond, templateRepoGuard))
			if rest == "" {
				continue
			}
			if !strings.HasPrefix(rest, "&&") {
				t.Errorf("%s job %s: the guard must be joined with &&: %q", name, job, cond)
			}
			if topLevelOr(rest) {
				t.Errorf("%s job %s: an || outside parentheses escapes the guard: %q", name, job, cond)
			}
		}
	}
}

// topLevelOr reports whether s has an || outside any parentheses.
func topLevelOr(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '|':
			if depth == 0 && i+1 < len(s) && s[i+1] == '|' {
				return true
			}
		}
	}
	return false
}

// Test_MctlImagePinned checks the image is the bare X.Y.Z MctlVersion tag; config derives the op.env ref from it.
func Test_MctlImagePinned(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(file.MctlVersion) {
		t.Fatalf("MctlVersion = %q, want a bare X.Y.Z image tag", file.MctlVersion)
	}
	want := "${{ vars.MCTL_IMAGE || 'gsoci.azurecr.io/giantswarm/mctl:" + file.MctlVersion + "' }}"

	var pinned int
	for name, body := range mctlRenders(t, false) {
		if strings.Contains(body, "mctl:latest") {
			t.Errorf("%s still runs mctl:latest", name)
		}
		image, ok := parseMctl(t, name, body).Env["MCTL_IMAGE"]
		if !ok {
			continue
		}
		pinned++
		if image != want {
			t.Errorf("%s MCTL_IMAGE = %q, want %q", name, image, want)
		}
	}
	if pinned != 6 {
		t.Errorf("%d workflows set MCTL_IMAGE, want 6 (all but the merge gate and cascades)", pinned)
	}
}

// Test_MctlNoOpEnvInRepo checks op.env is fetched from giantswarm/mctl, never read from the repo.
func Test_MctlNoOpEnvInRepo(t *testing.T) {
	script := mctlStepScript(t, "config", "Fetch op.env")
	if !strings.Contains(script, "repos/giantswarm/mctl/contents/ci/op.env?ref=$ref") {
		t.Errorf("config does not fetch ci/op.env from giantswarm/mctl:\n%s", script)
	}
	for name, body := range mctlRenders(t, false) {
		if strings.Contains(body, ".github/workflows/op.env") || strings.Contains(body, "op://") {
			t.Errorf("%s references a repository op.env or a secret path", name)
		}
	}
}

// Test_MctlMergeGatePassesOtherPRs checks the gate runs on every PR and posts success for non-init PRs.
func Test_MctlMergeGatePassesOtherPRs(t *testing.T) {
	body := mctlRenders(t, false)["merge_gate"]
	wf := parseMctl(t, "merge_gate", body)

	gate, ok := wf.Jobs["gate"]
	if !ok {
		t.Fatalf("no gate job in merge_gate")
	}
	for _, filter := range []string{"head_ref", "head.ref", "actor", "user.login"} {
		if strings.Contains(gate.If, filter) {
			t.Errorf("gate if = %q filters on %s: a skipped gate leaves the required check pending", gate.If, filter)
		}
	}

	m := regexp.MustCompile(`const match = branch\.match\(/(.+?)/\);`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no init branch regex in merge_gate; the non-init short-circuit has moved")
	}
	initBranch := regexp.MustCompile(m[1])

	for _, branch := range []string{"teams-alignment-branch", "renovate/giantswarm-goten", "main-update", "fix-init-script"} {
		if initBranch.MatchString(branch) {
			t.Errorf("%q is treated as a cluster init PR and gated", branch)
		}
	}
	for _, branch := range []string{"jackal-init", "jackal-init-capz", "xatest3-init-capa"} {
		if !initBranch.MatchString(branch) {
			t.Errorf("%q is not treated as a cluster init PR", branch)
		}
	}

	pass := strings.Index(body, "await publish('success', 'Not a cluster init PR")
	firstFailure := strings.Index(body, "await publish('failure'")
	if pass < 0 || firstFailure < 0 || pass > firstFailure {
		t.Error("the merge gate must post success for a non-init PR before any failure path")
	}
}

// Test_MctlMergeGateStatusUsesGithubToken checks the status is posted as the GitHub Actions app, which required checks are pinned to.
func Test_MctlMergeGateStatusUsesGithubToken(t *testing.T) {
	body := mctlRenders(t, false)["merge_gate"]
	wf := parseMctl(t, "merge_gate", body)

	var publishers int
	for _, step := range wf.Jobs["gate"].Steps {
		script := step.With["script"]
		if !strings.Contains(script, "createCommitStatus") {
			continue
		}
		publishers++
		if got := step.With["github-token"]; got != "${{ github.token }}" {
			t.Errorf("step %q posts the status with github-token %q, want ${{ github.token }}", step.Name, got)
		}
		if strings.Contains(script, "crossRepo.rest.repos.createCommitStatus") {
			t.Errorf("step %q posts the status with the App token client", step.Name)
		}
	}
	if publishers != 1 {
		t.Errorf("%d steps post a commit status, want 1", publishers)
	}

	var perms struct {
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(body), &perms); err != nil {
		t.Fatal(err)
	}
	if got := perms.Jobs["gate"].Permissions["statuses"]; got != "write" {
		t.Errorf("gate job permissions.statuses = %q, want write", got)
	}
}

// Test_MctlNoExpressionInRun checks untrusted input and tokens reach run: scripts only via env:.
func Test_MctlNoExpressionInRun(t *testing.T) {
	forbidden := regexp.MustCompile(`\$\{\{\s*(github\.head_ref|github\.event\.comment\.body|github\.event\.pull_request\.title|github\.event\.pull_request\.head\.ref|steps\.[\w-]+\.outputs\.token)`)

	for name, body := range mctlRenders(t, false) {
		for job, j := range parseMctl(t, name, body).Jobs {
			for _, step := range j.Steps {
				if m := forbidden.FindString(step.Run); m != "" {
					t.Errorf("%s job %s step %q interpolates %s}} into its run script; pass it through env:", name, job, step.Name, m)
				}
			}
		}
	}
}

// Test_MctlScrapesMctlOutput pins the mctl output strings the workflows match on (see mctl's pkg/manifest).
func Test_MctlScrapesMctlOutput(t *testing.T) {
	renders := mctlRenders(t, false)
	for name, want := range map[string]string{
		"init":              "Fill them in by commenting:",
		"validate_manifest": "manifest still contains placeholder values",
	} {
		if !strings.Contains(renders[name], want) {
			t.Errorf("%s does not match on %q", name, want)
		}
	}
}

// Test_MctlConfigParsesHyphenatedSkippedSteps checks the skipped-step parser keeps hyphenated names.
func Test_MctlConfigParsesHyphenatedSkippedSteps(t *testing.T) {
	m := regexp.MustCompile(`matchAll\(/(.+?)/g\)`).FindStringSubmatch(mctlRenders(t, false)["config"])
	if m == nil {
		t.Fatal("no matchAll(/.../g) literal in config; the skipped-step parser has moved")
	}
	re, err := regexp.Compile(m[1])
	if err != nil {
		t.Fatalf("skipped-step regex does not compile: %v", err)
	}

	got := re.FindStringSubmatch("[etcd-backup] Reason text.\n  → mctl config --etcd-backup\n")
	if got == nil || got[1] != "etcd-backup" {
		t.Errorf("regex %q captured %q, want etcd-backup", m[1], got)
	}
}

// Test_MctlCommentFlagParsing runs the workflows' flag parsing: CRLF stripped, trailing text ignored, reserved flags refused.
func Test_MctlCommentFlagParsing(t *testing.T) {
	const (
		patchInitStep = "Parse branch and comment flags"
		configStep    = "Parse comment flags"
	)
	initBranch := map[string]string{"BRANCH": "jackal-init-capz"}

	for _, tc := range []struct {
		workflow, step string
		env            map[string]string
		comment        string
		want           string
		wantRefused    bool
	}{
		{workflow: "patch_init", step: patchInitStep, env: initBranch, comment: "/init --base-domain gksoftware.gigantic.io --pipeline testing\r\n\r\n", want: "--base-domain gksoftware.gigantic.io --pipeline testing"},
		{workflow: "patch_init", step: patchInitStep, env: initBranch, comment: "/init   --location westeurope\r\nplease and thank you\r\n", want: "--location westeurope"},
		{workflow: "config", step: configStep, comment: "/config --credentials mctl-azure-gk-software\r\n", want: "--credentials mctl-azure-gk-software"},
		{workflow: "config", step: configStep, comment: "/config\r\n", want: ""},
		{workflow: "patch_init", step: patchInitStep, env: initBranch, comment: "/init --cmc-repo victim-management-clusters\r\n", wantRefused: true},
		{workflow: "patch_init", step: patchInitStep, env: initBranch, comment: "/init --ccr-repo=victim-configs\r\n", wantRefused: true},
		{workflow: "config", step: configStep, comment: "/config --cmc-repo victim-management-clusters\r\n", wantRefused: true},
		{workflow: "config", step: configStep, comment: "/config --source-pr https://evil/pr/1\r\n", wantRefused: true},
	} {
		t.Run(tc.workflow+": "+strings.TrimSpace(tc.comment), func(t *testing.T) {
			outPath := filepath.Join(t.TempDir(), "output")
			if err := os.WriteFile(outPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.CommandContext(t.Context(), "bash", "-eo", "pipefail", "-c", mctlStepScript(t, tc.workflow, tc.step)) // #nosec G204 -- the script is the rendered template, test-only
			cmd.Env = append(os.Environ(), "COMMENT="+tc.comment, "GITHUB_OUTPUT="+outPath)
			for k, v := range tc.env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			out, err := cmd.CombinedOutput()

			raw, readErr := os.ReadFile(outPath) // #nosec G304 -- the test's own temp file
			if readErr != nil {
				t.Fatal(readErr)
			}
			flags := outputValue(string(raw), "flags")

			if tc.wantRefused {
				if err == nil || !strings.Contains(string(out), "cannot be passed") {
					t.Fatalf("reserved flag not refused (err %v):\n%s", err, out)
				}
				if flags != "" {
					t.Errorf("flags were still written: %q", flags)
				}
				return
			}
			if err != nil {
				t.Fatalf("script failed: %v\n%s", err, out)
			}
			if flags != tc.want {
				t.Errorf("flags = %q, want %q", flags, tc.want)
			}
		})
	}
}

// Test_MctlValidateManifestSkipsLegacyCharts checks legacy charts are skipped, release-* and unparsable ones validated.
func Test_MctlValidateManifestSkipsLegacyCharts(t *testing.T) {
	if _, err := exec.LookPath("yq"); err != nil {
		t.Skip("yq not on PATH (it is on the GitHub-hosted runner)")
	}

	dir := t.TempDir()
	manifests := map[string]string{
		"management-clusters/legacy/cluster-app-manifests.yaml":  "apiVersion: application.giantswarm.io/v1alpha1\nkind: App\nspec:\n  name: cluster-aws\n---\nkind: ConfigMap\n",
		"management-clusters/release/cluster-app-manifests.yaml": "kind: ConfigMap\n---\napiVersion: application.giantswarm.io/v1alpha1\nkind: App\nspec:\n  name: release-aws\n",
		"management-clusters/broken/cluster-app-manifests.yaml":  "kind: App\nspec: [unclosed\n",
	}
	var list []string
	for path, body := range manifests {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		list = append(list, path)
	}

	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "docker-calls")
	stubs := map[string]string{
		"git":    "#!/bin/sh\nprintf '%s\\n' " + strings.Join(list, " ") + "\n",
		"docker": "#!/bin/sh\necho \"$@\" >> " + calls + "\necho valid\n",
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil { // #nosec G306 -- test stub must be executable
			t.Fatal(err)
		}
	}

	cmd := exec.CommandContext(t.Context(), "bash", "-eo", "pipefail", "-c", mctlStepScript(t, "validate_manifest", "Validate changed manifests")) // #nosec G204 -- the script is the rendered template, test-only
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "GITHUB_OUTPUT="+filepath.Join(dir, "output"), "MCTL_IMAGE=mctl:test", "BASE=a", "HEAD=b")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("validate step failed: %v\n%s", err, out)
	}

	got, err := os.ReadFile(calls) // #nosec G304 -- the test's own temp file
	if err != nil {
		t.Fatalf("mctl validate never ran:\n%s", out)
	}
	for _, want := range []string{"release/cluster-app-manifests.yaml", "broken/cluster-app-manifests.yaml"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("mctl validate did not run on %s:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "legacy/") {
		t.Errorf("mctl validate ran on the legacy-chart manifest:\n%s", got)
	}
	if !strings.Contains(string(out), `skipped: cluster chart "cluster-aws"`) {
		t.Errorf("no skip line for the legacy chart:\n%s", out)
	}
}

// outputValue reads a key out of a $GITHUB_OUTPUT file.
func outputValue(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok && name == key {
			return value
		}
	}
	return ""
}
