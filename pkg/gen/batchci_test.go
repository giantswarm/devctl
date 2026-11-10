package gen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// Exercise publication as well as transformation: alignment and a precommit
// render into another directory must retain the checked-in opt-in and gate.
func TestBatchPolicySurvivesRegeneration(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".devctl"), 0750); err != nil {
		t.Fatal(err)
	}
	policy := "mode: prepare\nbaseBranch: main\ngateAppID: 15368\nresults:\n- name: unit\n  appID: 15368\n  workflow: .github/workflows/unit.yaml\n"
	configPath := filepath.Join(root, ".devctl/ci-batch.yaml")
	if err := os.WriteFile(configPath, []byte(policy), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".github/workflows/zz_generated.pre-commit.yaml")
	gatePath := filepath.Join(root, ".github/workflows/zz_generated.batch_ci.yaml")
	source := "name: pre-commit\non:\n  pull_request: {}\n  push:\n    branches: [main]\njobs: {}\n"
	for _, mode := range []string{"prepare", "batch", "prepare"} {
		if err := os.WriteFile(configPath, []byte(strings.Replace(policy, "prepare", mode, 1)), 0600); err != nil {
			t.Fatal(err)
		}
		for round := 0; round < 2; round++ {
			if err := Execute(context.Background(), input.Input{Path: path, TemplateBody: source}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "pull_request:") != (mode == "prepare") {
				t.Fatalf("wrong mode: %s", data)
			}
			if !strings.Contains(string(data), "merge_group:") {
				t.Fatal("missing queue trigger")
			}
			gate, err := os.ReadFile(gatePath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(gate), `"mode":"`+mode+`"`) {
				t.Fatalf("gate not regenerated: %s", gate)
			}
		}
	}
	// Removing the policy and regenerating restores ordinary PR CI; the gate
	// itself is deliberately removed only after its server requirement is gone.
	if err := os.Rename(configPath, configPath+".disabled"); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), input.Input{Path: path, TemplateBody: source}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != source {
		t.Fatalf("default output changed: %s", got)
	}
}
