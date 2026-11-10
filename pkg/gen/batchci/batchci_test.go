package batchci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const policy = `mode: prepare
baseBranch: main
gateAppID: 15368
results:
- name: pre-commit
  appID: 15368
  workflow: .github/workflows/zz_generated.pre-commit.yaml
- name: "ci/circleci: build"
  creatorID: 123
`

func TestPolicy(t *testing.T) {
	for _, data := range []string{
		strings.Replace(policy, "prepare", "automatic", 1),
		policy + "unknown: true\n",
		strings.Replace(policy, "creatorID: 123", "appID: 123\n  creatorID: 123", 1),
		strings.Replace(policy, "name: pre-commit", "name: batch-ci", 1),
		strings.Replace(policy, "  workflow: .github/workflows/zz_generated.pre-commit.yaml\n", "", 1),
		strings.Replace(policy, "ci/circleci: build", "pre-commit", 1),
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Fatalf("accepted invalid policy:\n%s", data)
		}
	}
	if _, err := Parse([]byte(policy)); err != nil {
		t.Fatal(err)
	}
}

func TestActionsTransition(t *testing.T) {
	c, err := Parse([]byte(policy))
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("name: pre-commit\non:\n  pull_request:\n    paths: [src/**]\n  push:\n    branches: [main]\n  schedule:\n  - cron: '0 0 * * *'\nconcurrency:\n  cancel-in-progress: ${{ github.event_name == 'pull_request' }}\njobs: {}\n")
	path := ".github/workflows/zz_generated.pre-commit.yaml"
	unchanged, err := Apply(path, source, nil)
	if err != nil || !bytes.Equal(unchanged, source) {
		t.Fatal("default changed")
	}
	for _, mode := range []string{"prepare", "batch"} {
		c.Mode = mode
		got, err := Apply(path, source, c)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(got, &doc); err != nil {
			t.Fatal(err)
		}
		on := field(doc.Content[0], "on")
		if (field(on, "pull_request") != nil) != (mode == "prepare") {
			t.Fatalf("wrong PR transition: %s", got)
		}
		if field(on, "merge_group") == nil || field(on, "push") == nil || field(on, "schedule") == nil {
			t.Fatalf("lost trigger: %s", got)
		}
		if field(field(doc.Content[0], "concurrency"), "cancel-in-progress").Value != "${{ github.event_name == 'pull_request' }}" {
			t.Fatal("changed cancellation")
		}
		for _, name := range []string{"semantic_pull_request", "auto_release", "create_release", "helm_docs_regen"} {
			untouched, err := Apply(".github/workflows/zz_generated."+name+".yaml", source, c)
			if err != nil || !bytes.Equal(untouched, source) {
				t.Fatalf("changed %s", name)
			}
		}
	}
}

func TestCircleSetupTransition(t *testing.T) {
	c, err := Parse([]byte(policy))
	if err != nil {
		t.Fatal(err)
	}
	for _, template := range []string{"setup-config.yml.template", "multi-image-setup-config.yml.template"} {
		source, err := os.ReadFile(filepath.Join("..", "input", "circleci", "internal", "file", template))
		if err != nil {
			t.Fatal(err)
		}
		// No template execution is needed to check the setup structure. Values
		// containing Go actions are scalar template arguments in these documents.
		for _, mode := range []string{"prepare", "batch"} {
			c.Mode = mode
			got, err := Apply(".circleci/config.yml", source, c)
			if err != nil {
				t.Fatal(err)
			}
			var doc yaml.Node
			if err := yaml.Unmarshal(got, &doc); err != nil {
				t.Fatal(err)
			}
			steps := field(field(field(doc.Content[0], "jobs"), "setup"), "steps")
			if steps.Content[0].Value != "checkout" {
				t.Fatal("lost checkout")
			}
			command := field(field(steps.Content[1], "run"), "command").Value
			if !strings.Contains(command, `test "$(git rev-parse HEAD)" = "$CIRCLE_SHA1"`) {
				t.Fatal("no exact checkout guard")
			}
			if strings.Contains(command, "circleci-agent step halt") != (mode == "batch") {
				t.Fatal("unsafe transition")
			}
			if !bytes.Contains(got, []byte("continuation/continue")) || !bytes.Contains(got, []byte(".circleci/custom.yml")) {
				t.Fatal("lost custom continuation")
			}
		}
	}
}

func TestQueueGateSemantics(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is needed for the generated github-script contract test")
	}
	cmd := exec.Command(node, "gate_test.cjs") // #nosec G204 -- fixed test harness, resolved node executable
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("queue gate contract: %v\n%s", err, output)
	}
}

func TestCircleAdmissionBoundaries(t *testing.T) {
	c, err := Parse([]byte(policy))
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("version: 2.1\njobs:\n  setup:\n    steps:\n    - checkout\n    - run: echo continuation\n")
	for _, mode := range []string{"prepare", "batch"} {
		c.Mode = mode
		data, err := Apply(".circleci/config.yml", source, c)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		steps := field(field(field(doc.Content[0], "jobs"), "setup"), "steps")
		command := field(field(steps.Content[1], "run"), "command").Value
		for _, tc := range []struct {
			name, branch, pr, tag, sha string
			halt, fail                 bool
		}{
			{name: "PR", branch: "feature", pr: "https://example.invalid/pull/1", sha: "exact", halt: mode == "batch"},
			{name: "no metadata", branch: "feature", sha: "exact"},
			{name: "queue", branch: "gh-readonly-queue/main/pr-1", pr: "PR", sha: "exact"},
			{name: "base", branch: "main", pr: "PR", sha: "exact"},
			{name: "maintenance", branch: "release-8.x", pr: "PR", sha: "exact"},
			{name: "tag", branch: "feature", pr: "PR", tag: "v1.0.0", sha: "exact"},
			{name: "wrong queue checkout", branch: "gh-readonly-queue/main/pr-1", sha: "wrong", fail: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				// Shell functions model the Circle agent and git; no repository, token,
				// pipeline or external service is touched by this admission test.
				script := "git() { echo exact; }; circleci-agent() { echo HALTED; };\n" + command
				cmd := exec.Command("bash", "-c", script) // #nosec G204 -- fixed generated admission script with synthetic inputs
				cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "CIRCLE_BRANCH=" + tc.branch, "CIRCLE_PULL_REQUEST=" + tc.pr, "CIRCLE_TAG=" + tc.tag, "CIRCLE_SHA1=" + tc.sha}
				out, err := cmd.CombinedOutput()
				if (err != nil) != tc.fail {
					t.Fatalf("unexpected admission error: %v: %s", err, out)
				}
				if strings.Contains(string(out), "HALTED") != tc.halt {
					t.Fatalf("unexpected halt: %s", out)
				}
			})
		}
	}
}
