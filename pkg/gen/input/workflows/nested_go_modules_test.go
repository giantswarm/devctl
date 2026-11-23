package workflows

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func nestedModuleStep(t *testing.T, job, id string) string {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []autoReleaseStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(renderInput(t, newWorkflows(t).NestedGoModules())), &workflow))
	for _, step := range workflow.Jobs[job].Steps {
		if step.ID == id {
			return step.Run
		}
	}
	t.Fatalf("missing step %s/%s", job, id)
	return ""
}

func TestNestedModuleDiscovery(t *testing.T) {
	directory := t.TempDir()
	mustGit(t, directory, "init", "-q", "--initial-branch=main")
	for _, path := range []string{"go.mod", "k8s/go.mod", "sdk/native/go.mod", "testdata/example/go.mod", "vendor/dependency/go.mod", "node_modules/example/go.mod", ".cache/example/go.mod", "invalid path/go.mod", "untracked/go.mod"} {
		writeRepoFile(t, filepath.Join(directory, path), "module example.test/module\n")
		if path != "untracked/go.mod" {
			mustGit(t, directory, "add", "--", path)
		}
	}
	output, log, err := runStep(t, nestedModuleStep(t, "modules", "discover"), directory)
	require.NoError(t, err, log)
	require.JSONEq(t, `["k8s","sdk/native"]`, output["paths"])
	empty := t.TempDir()
	mustGit(t, empty, "init", "-q", "--initial-branch=main")
	output, log, err = runStep(t, nestedModuleStep(t, "modules", "discover"), empty)
	require.NoError(t, err, log)
	require.JSONEq(t, `[]`, output["paths"])
}

func TestNestedModuleReleaseValidation(t *testing.T) {
	for _, test := range []struct {
		path, version string
		valid         bool
	}{
		{"k8s", "v0.1.0", true}, {"sdk/native", "v2.0.0-rc.1", true},
		{".", "v1.0.0", false}, {"../k8s", "v0.1.0", false}, {"unknown", "v0.1.0", false},
		{"k8s", "v01.0.0", false}, {"k8s", "v0.1", false}, {"k8s", "v0.1.0;touch bad", false},
		{"k8s", "v0.1.0-rc.01", false}, {"k8s", "v0.1.0-rc.0", true},
	} {
		t.Run(test.path+"/"+test.version, func(t *testing.T) {
			_, log, err := runStep(t, nestedModuleStep(t, "release", "validate"), t.TempDir(),
				"MODULE_PATH="+test.path, "MODULE_VERSION="+test.version, `MODULE_PATHS=["k8s","sdk/native"]`)
			if test.valid {
				require.NoError(t, err, log)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNestedModuleReleaseNeverMovesTag(t *testing.T) {
	directory := t.TempDir()
	mustGit(t, directory, "init", "-q", "--initial-branch=main")
	writeRepoFile(t, filepath.Join(directory, "k8s/go.mod"), "module github.com/giantswarm/example/k8s\n\ngo 1.26.0\n")
	mustGit(t, directory, "add", ".")
	mustGit(t, directory, "commit", "-qm", "feat: module")
	mustGit(t, directory, "tag", "k8s/v0.1.0")
	mustGit(t, directory, "commit", "--allow-empty", "-qm", "fix: subsequent change")
	header := mustGit(t, directory, "rev-parse", "HEAD")
	bin := t.TempDir()
	writeRepoFile(t, filepath.Join(bin, "go"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' github.com/giantswarm/example/k8s\n")
	require.NoError(t, os.Chmod(filepath.Join(bin, "go"), 0o700)) // #nosec G302 -- test executable
	_, log, err := runStep(t, nestedModuleStep(t, "release", "publish"), directory,
		"MODULE_PATH=k8s", "MODULE_VERSION=v0.1.0", "GITHUB_SHA="+header, "GITHUB_REPOSITORY=giantswarm/example", "PATH="+bin+":"+os.Getenv("PATH"))
	require.Error(t, err, log)
	require.NotEqual(t, header, mustGit(t, directory, "rev-parse", "k8s/v0.1.0"))
}

func TestNestedModuleWorkflowPermissions(t *testing.T) {
	var workflow struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			If          string            `yaml:"if"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(renderInput(t, newWorkflows(t).NestedGoModules())), &workflow))
	require.Empty(t, workflow.Permissions)
	require.Equal(t, "read", workflow.Jobs["modules"].Permissions["contents"])
	require.Equal(t, "read", workflow.Jobs["test"].Permissions["contents"])
	require.Equal(t, "write", workflow.Jobs["release"].Permissions["contents"])
	require.Equal(t, "github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main'", workflow.Jobs["release"].If)
}

func TestNestedModuleVerifySkipsClusterTestsOnly(t *testing.T) {
	const clusterTest = `package e2e

import "testing"

func TestNeedsCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a live management cluster")
	}
	t.Fatal("ran without a management cluster")
}
`
	const failingTest = `
func TestUnit(t *testing.T) {
	t.Fatal("unit test failure")
}
`
	testCases := []struct {
		name    string
		tests   string
		wantErr bool
	}{
		{name: "a test that needs a cluster skips in short mode", tests: clusterTest},
		{name: "other tests still run", tests: clusterTest + failingTest, wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			mustGit(t, directory, "init", "-q", "--initial-branch=main")
			writeRepoFile(t, filepath.Join(directory, "tests/e2e/go.mod"), "module e2e\n\ngo 1.21\n")
			writeRepoFile(t, filepath.Join(directory, "tests/e2e/e2e_test.go"), tc.tests)
			mustGit(t, directory, "add", "--", ".")

			_, log, err := runStep(t, nestedModuleStep(t, "test", "verify"), filepath.Join(directory, "tests/e2e"), "GOWORK=off")
			if tc.wantErr {
				require.Error(t, err, log)
				require.Contains(t, log, "unit test failure")
				return
			}
			require.NoError(t, err, log)
		})
	}
}
