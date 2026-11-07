package makefile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci"
)

// Both executables are fixtures: neither Docker nor a cluster is contacted.
// Record each argument separately to catch quoting errors, and check that the
// chart and kubeconfig passed to the container really exist inside its mount.
const fakeATSTool = `#!/bin/sh
set -eu
tool=${0##*/}
printf '%s\n' "$tool" "$@" '--end--' >> "$ATS_TEST_CALLS"
case "$tool:$1" in
kind:get)
	[ "$ATS_TEST_FAILURE" != get ] || exit 11
	if [ "$ATS_TEST_FAILURE" = collision ]; then
		for work in .ats-run.*; do
			printf 'ats-%s\n' "${work##*.}" | tr '[:upper:]' '[:lower:]'
		done
	fi
	exit 0;;
kind:create)
	[ "$ATS_TEST_FAILURE" != create ] || exit 13
	while [ "$1" != --kubeconfig ]; do shift; done
	printf fixture-kubeconfig > "$2"
	exit 0;;
kind:delete)
	[ "$ATS_TEST_FAILURE" != delete ] || exit 19
	exit 0;;
docker:run)
	while [ "$1" != --chart-file ]; do shift; done
	[ "$(cat "$2")" = fixture-chart ] || exit 20
	shift 2
	[ "$1" = --cluster-kubeconfig ] || exit 21
	[ "$(cat "$2")" = fixture-kubeconfig ] || exit 22
	[ "$ATS_TEST_FAILURE" != docker ] || exit 17
	exit 0;;
*) exit 23;;
esac
`

func Test_ATSTest(t *testing.T) {
	const (
		smokeTestPath  = "tests/ats/test_smoke.py"
		noSuiteMessage = "no ATS suite"
		allStages      = "get create docker delete"
		chartPath      = "build/chart.tgz"
		getStage       = "get"
		createStage    = "create"
		dockerStage    = "docker"
	)

	if _, err := exec.LookPath("make"); err != nil {
		t.Fatal("make is required for the ATS target regression")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("the recording tool fixtures require a POSIX shell")
	}

	m, err := New(Config{Flavours: gen.FlavourSlice{gen.FlavourApp}})
	if err != nil {
		t.Fatal(err)
	}
	in := m.MakefileGenApp()
	tpl, err := template.New("makefile").Parse(in.TemplateBody)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{
		"# renovate: datasource=docker depName=" + circleci.ATSImageRegistry + "/giantswarm/app-test-suite\nATS_VERSION ?= " + circleci.DefaultATSVersion,
		"# renovate: datasource=docker depName=" + circleci.ATSImageRegistry + "/giantswarm/kind-node\nATS_KIND_NODE_IMAGE_VERSION ?= " + circleci.ATSKindNodeImageVersion,
	} {
		if !strings.Contains(rendered.String(), pin+"\n") {
			t.Fatalf("generated ATS pin must match the CI default and carry a Renovate annotation: %q", pin)
		}
	}

	testCases := []struct {
		name       string
		files      []string
		args       []string
		custom     string
		failure    string
		wantStages string
		wantConfig string
		wantFlags  []string
		wantImage  string
		wantTag    string
		wantMsg    string
	}{
		{
			name:    "case 0: no suite needs no tools or chart",
			wantMsg: noSuiteMessage,
		},
		{
			name:       "case 1: default CI invocation with a suite but no config",
			files:      []string{smokeTestPath, "build/nested/chart.tgz"},
			wantStages: allStages,
		},
		{
			name:       "case 2: repo config selects a custom suite and kind shape",
			files:      []string{".ats/main.yaml", ".ats/kind-config.yaml", chartPath},
			wantStages: allStages,
			wantConfig: circleci.ATSKindConfigPath,
		},
		{
			name:       "case 3: main.yml is also an ATS suite signal",
			files:      []string{".ats/main.yml", chartPath},
			wantStages: allStages,
		},
		{
			name:       "case 4: explicit archive and late custom overrides",
			files:      []string{smokeTestPath, "out/custom chart.tgz", "build/one.tgz", "build/two.tgz", "config/kind config.yaml"},
			args:       []string{"ATS_CHART_FILE=out/custom chart.tgz"},
			custom:     "ATS_VERSION := 1.0.2\nATS_KIND_NODE_IMAGE := mirror.example/kind:v1.36.4@sha256:abc\nATS_KIND_CONFIG := config/kind config.yaml\nATS_FLAGS := --steps smoke functional\n",
			wantStages: allStages,
			wantConfig: "config/kind config.yaml",
			wantFlags:  []string{"--steps", "smoke", "functional"},
			wantImage:  "mirror.example/kind:v1.36.4@sha256:abc",
			wantTag:    "1.0.2",
		},
		{
			name:       "case 5: archive prefix selects one chart",
			files:      []string{smokeTestPath, "build/other.tgz", "build/selected-1.tgz"},
			args:       []string{"ATS_CHART_ARCHIVE_PREFIX=selected-"},
			wantStages: allStages,
		},
		{
			name:    "case 6: ambiguous build artifacts stop before kind",
			files:   []string{smokeTestPath, "build/one.tgz", "build/two.tgz"},
			wantMsg: "expected one built chart",
		},
		{
			name:    "case 7: missing build stops before kind",
			files:   []string{smokeTestPath},
			wantMsg: "expected one built chart",
		},
		{
			name:    "case 8: missing explicit chart stops before kind",
			files:   []string{smokeTestPath},
			args:    []string{"ATS_CHART_FILE=missing.tgz"},
			wantMsg: "ATS_CHART_FILE 'missing.tgz' does not exist",
		},
		{
			name:    "case 9: missing explicit kind config stops before kind",
			files:   []string{smokeTestPath, chartPath},
			args:    []string{"ATS_KIND_CONFIG=missing.yaml"},
			wantMsg: "ATS_KIND_CONFIG 'missing.yaml' does not exist",
		},
		{
			name:    "case 10: legacy version refuses the incompatible invocation",
			files:   []string{smokeTestPath},
			args:    []string{"ATS_VERSION=0.15.0"},
			wantMsg: "ats-test requires ATS 1.x",
		},
		{
			name:       "case 11: failed cluster discovery does not delete anything",
			files:      []string{smokeTestPath, chartPath},
			failure:    getStage,
			wantStages: getStage,
			wantMsg:    "Error 11",
		},
		{
			name:       "case 12: an existing cluster is never replaced or deleted",
			files:      []string{smokeTestPath, chartPath},
			failure:    "collision",
			wantStages: getStage,
			wantMsg:    "already exists; refusing to replace it",
		},
		{
			name:       "case 13: failed creation cleans up the partial cluster",
			files:      []string{smokeTestPath, chartPath},
			failure:    createStage,
			wantStages: "get create delete",
			wantMsg:    "Error 13",
		},
		{
			name:       "case 14: ATS failure survives successful cleanup",
			files:      []string{smokeTestPath, chartPath},
			failure:    dockerStage,
			wantStages: allStages,
			wantMsg:    "Error 17",
		},
		{
			name:       "case 15: cleanup failure fails a successful ATS run",
			files:      []string{smokeTestPath, chartPath},
			failure:    "delete",
			wantStages: allStages,
			wantMsg:    "Error 1",
		},
		{
			name:    "case 16: generated dependency files alone are not a suite",
			files:   []string{"tests/ats/pyproject.toml", "tests/ats/uv.lock", "tests/ats/Pipfile"},
			wantMsg: noSuiteMessage,
		},
		{
			name:       "case 17: Go suites need no ATS config",
			files:      []string{"tests/ats/smoke/app_test.go", chartPath},
			wantStages: allStages,
		},
		{
			name:       "case 18: pytest suffix pattern also selects a suite",
			files:      []string{"tests/ats/smoke/app_test.py", chartPath},
			wantStages: allStages,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "repo with spaces")
			write := func(path, content string, mode os.FileMode) {
				t.Helper()
				p := filepath.Join(dir, path)
				if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			write("Makefile", "include Makefile.*.mk\n", 0o600)
			write(in.Path, rendered.String(), 0o600)
			write("Makefile.zzz.custom.mk", tc.custom, 0o600)
			write("bin/kind", fakeATSTool, 0o700)   // #nosec G306 -- executable test fixture
			write("bin/docker", fakeATSTool, 0o700) // #nosec G306 -- executable test fixture
			write("user-kubeconfig", "untouched", 0o600)
			write("kube.config", "untouched", 0o600)
			for _, file := range tc.files {
				write(file, "fixture-chart", 0o600)
			}
			calls := filepath.Join(dir, "calls")
			write("calls", "", 0o600)
			cmd := exec.Command("make", append([]string{"ats-test"}, tc.args...)...) // #nosec G204 -- fixed command, fixture arguments
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"KUBECONFIG="+filepath.Join(dir, "user-kubeconfig"),
				"ATS_TEST_CALLS="+calls,
				"ATS_TEST_FAILURE="+tc.failure,
				"LC_ALL=C",
			)
			out, err := cmd.CombinedOutput()
			wantErr := tc.wantMsg != "" && tc.wantMsg != noSuiteMessage
			if (err != nil) != wantErr {
				t.Fatalf("make ats-test: err = %v, want error %v\n%s", err, wantErr, out)
			}
			if tc.wantMsg != "" && !strings.Contains(string(out), tc.wantMsg) {
				t.Errorf("missing message %q:\n%s", tc.wantMsg, out)
			}

			nodeImage := tc.wantImage
			if nodeImage == "" {
				nodeImage = circleci.ATSImageRegistry + "/giantswarm/kind-node:" + circleci.ATSKindNodeImageVersion
			}
			version := strings.TrimPrefix(strings.Split(strings.Split(nodeImage, "@")[0], ":")[1], "v")
			tag := tc.wantTag
			if tag == "" {
				tag = circleci.DefaultATSVersion
			}
			record := func(args ...string) string { return strings.Join(append(args, "--end--"), "\n") + "\n" }
			createArgs := []string{"kind", createStage, "cluster", "--name", "<cluster>", "--image", nodeImage}
			if tc.wantConfig != "" {
				createArgs = append(createArgs, "--config", tc.wantConfig)
			}
			createArgs = append(createArgs, "--kubeconfig", "<work>/kube.config", "--wait", "120s")
			runArgs := []string{
				dockerStage, "run", "--rm", "-e", "USE_UID=" + strconv.Itoa(os.Getuid()), "-e", "USE_GID=" + strconv.Itoa(os.Getgid()),
				"-v", dir + ":/ats/workdir", "--network", "host", circleci.ATSImageRegistry + "/giantswarm/app-test-suite:" + tag,
				"--chart-file", "<work>/chart.tgz", "--cluster-kubeconfig", "<work>/kube.config", "--cluster-type", "kind", "--cluster-version", version,
			}
			stages := map[string]string{
				getStage:    record("kind", getStage, "clusters"),
				createStage: record(createArgs...),
				dockerStage: record(append(runArgs, tc.wantFlags...)...),
				"delete":    record("kind", "delete", "cluster", "--name", "<cluster>", "--kubeconfig", "<work>/kube.config"),
			}
			var want strings.Builder
			for _, stage := range strings.Fields(tc.wantStages) {
				want.WriteString(stages[stage])
			}
			got, err := os.ReadFile(calls) // #nosec G304 -- fixture log in t.TempDir
			if err != nil {
				t.Fatal(err)
			}
			normalized := regexp.MustCompile(`\.ats-run\.[A-Za-z0-9]+`).ReplaceAllString(string(got), "<work>")
			normalized = regexp.MustCompile(`ats-[a-z0-9]{6}`).ReplaceAllString(normalized, "<cluster>")
			if diff := cmp.Diff(want.String(), normalized); diff != "" {
				t.Errorf("tool calls (-want +got):\n%s", diff)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, ".ats-run.*"))
			if err != nil || len(leftovers) != 0 {
				t.Errorf("temporary run directories remain: %v (%v)", leftovers, err)
			}
			for _, path := range []string{"user-kubeconfig", "kube.config"} {
				data, err := os.ReadFile(filepath.Join(dir, path)) // #nosec G304 -- fixture sentinel in t.TempDir
				if err != nil || string(data) != "untouched" {
					t.Errorf("%s changed: %q (%v)", path, data, err)
				}
			}
		})
	}
}
