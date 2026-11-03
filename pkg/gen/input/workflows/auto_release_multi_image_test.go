package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const goldenAutoReleaseMultiImagePath = "testdata/auto_release_multi_image.yaml.golden"

func Test_GoldenAutoReleaseMultiImage(t *testing.T) {
	got := renderInput(t, withFixedHeader(t, newWorkflows(t, gen.FlavourMultiImage).AutoReleaseMultiImage()))

	assertGolden(t, goldenAutoReleaseMultiImagePath, got)
}

// multiImageTagStep returns the script of the multi-image tag job's tagging
// step, the one that ships.
func multiImageTagStep(t *testing.T) string {
	t.Helper()

	rendered := renderInput(t, newWorkflows(t, gen.FlavourMultiImage).AutoReleaseMultiImage())
	var wf struct {
		Jobs map[string]struct {
			Steps []autoReleaseStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}
	for _, s := range wf.Jobs["tag"].Steps {
		if s.ID == "tag" {
			return s.Run
		}
	}

	t.Fatalf("no step with id \"tag\" in the tag job:\n%s", rendered)
	return ""
}

// multiImageHistory is a multi-image repository with an origin: the images a
// and b released as a/v0.1.0 and b/v0.1.0, the shared build files under
// toolchains/lib, and a pushed main.
func multiImageHistory(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir := filepath.Join(root, "repo")
	mustGit(t, root, "init", "-q", "--bare", "--initial-branch=main", origin)
	mustGit(t, root, "clone", "-q", origin, dir)
	for name, content := range map[string]string{
		"README.md":               "repo\n",
		"toolchains/a/Dockerfile": "FROM scratch\n",
		"toolchains/b/Dockerfile": "FROM scratch\n",
		"toolchains/lib/fetch.sh": "true\n",
	} {
		writeRepoFile(t, filepath.Join(dir, name), content)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "feat: the images")
	mustGit(t, dir, "tag", "a/v0.1.0")
	mustGit(t, dir, "tag", "b/v0.1.0")
	mustGit(t, dir, "push", "-q", "origin", "HEAD:main", "--tags")

	return dir
}

func writeRepoFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G703 -- t.TempDir() path, test-only
		t.Fatalf("write %s: %v", path, err)
	}
}

// releaseStubs puts a gh that records each call and a sleep that does not
// wait first on PATH; it returns the PATH entry and the call log.
func releaseStubs(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	log := filepath.Join(dir, "gh.log")
	for name, script := range map[string]string{
		"gh":    "#!/usr/bin/env bash\necho \"$*\" >> '" + log + "'\n",
		"sleep": "#!/usr/bin/env bash\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil { // #nosec G306 -- test-only executable stub
			t.Fatalf("write %s stub: %v", name, err)
		}
	}

	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"), log
}

func Test_AutoReleaseMultiImageTags(t *testing.T) {
	tests := []struct {
		name    string
		change  map[string]string
		remove  []string
		message string
		gate    string
		want    []string
		fails   bool
	}{
		{
			name:    "a feat releases the minor of the image it changed",
			change:  map[string]string{"toolchains/a/Dockerfile": "FROM scratch\nLABEL x=y\n"},
			message: "feat(a): add a tool",
			want:    []string{"a/v0.2.0"},
		},
		{
			name:    "a fix releases the patch",
			change:  map[string]string{"toolchains/b/Dockerfile": "FROM scratch\nLABEL x=y\n"},
			message: "fix: b",
			want:    []string{"b/v0.1.1"},
		},
		{
			name:    "a breaking change to the shared build files releases the major of every image",
			change:  map[string]string{"toolchains/lib/fetch.sh": "false\n"},
			message: "refactor!: fetch differently",
			want:    []string{"a/v1.0.0", "b/v1.0.0"},
		},
		{
			name:    "a breaking footer releases the major",
			change:  map[string]string{"toolchains/a/Dockerfile": "FROM scratch\nLABEL x=z\n"},
			message: "fix: a\n\nBREAKING CHANGE: the tool moved",
			want:    []string{"a/v1.0.0"},
		},
		{
			name:    "a new image starts at v0.1.0",
			change:  map[string]string{"toolchains/c/Dockerfile": "FROM scratch\n"},
			message: "feat: c",
			want:    []string{"c/v0.1.0"},
		},
		{
			name:    "a removed shared file releases every image",
			remove:  []string{"toolchains/lib/fetch.sh"},
			message: "chore: drop the shared fetch",
			want:    []string{"a/v0.1.1", "b/v0.1.1"},
		},
		{
			name:    "a removed image releases none of the others",
			remove:  []string{"toolchains/b/Dockerfile"},
			message: "chore: drop b",
		},
		{
			name:    "a new image releases none of the others",
			change:  map[string]string{"toolchains/c/Dockerfile": "FROM scratch\n", "toolchains/c/tool.sh": "true\n"},
			message: "fix: c",
			want:    []string{"c/v0.1.0"},
		},
		{
			name:    "a change outside the images releases nothing",
			change:  map[string]string{"README.md": "changed\n"},
			message: "fix: readme",
		},
		{
			name: "the repository's gate refuses one tag and releases the other",
			change: map[string]string{
				"toolchains/a/Dockerfile": "FROM scratch\nLABEL x=y\n",
				"toolchains/b/Dockerfile": "FROM scratch\nLABEL x=y\n",
			},
			message: "fix: both",
			gate:    "[ \"$1\" != a/v0.1.1 ]\n",
			want:    []string{"b/v0.1.1"},
			fails:   true,
		},
	}

	script := multiImageTagStep(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := multiImageHistory(t)
			for name, content := range tc.change {
				writeRepoFile(t, filepath.Join(dir, name), content)
			}
			for _, name := range tc.remove {
				mustGit(t, dir, "rm", "-q", name)
			}
			if tc.gate != "" {
				writeRepoFile(t, filepath.Join(dir, ".github/release-gate.custom.sh"), tc.gate)
			}
			mustGit(t, dir, "add", "-A")
			subject, body, _ := strings.Cut(tc.message, "\n\n")
			args := []string{"commit", "-q", "-m", subject}
			if body != "" {
				args = append(args, "-m", body)
			}
			mustGit(t, dir, args...)

			path, log := releaseStubs(t)
			_, out, err := runStep(t, script, dir, path, "IMAGES_DIR="+gen.MultiImageDir)
			if (err != nil) != tc.fails {
				t.Fatalf("tag step error = %v, want failure %v\n%s", err, tc.fails, out)
			}

			var got []string
			for _, line := range strings.Fields(mustGit(t, dir, "ls-remote", "--tags", "origin")) {
				if tag, ok := strings.CutPrefix(line, "refs/tags/"); ok && tag != "a/v0.1.0" && tag != "b/v0.1.0" {
					got = append(got, tag)
				}
			}
			slices.Sort(got)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("pushed tags = %v, want %v\n%s", got, tc.want, out)
			}

			listed, _ := os.ReadFile(filepath.Join(dir, "tags.txt")) // #nosec G304 -- t.TempDir() path, test-only
			if strings.Join(strings.Fields(string(listed)), ",") != strings.Join(tc.want, ",") {
				t.Errorf("tags.txt = %q, want %v", listed, tc.want)
			}

			calls, _ := os.ReadFile(log) // #nosec G304 -- t.TempDir() path, test-only
			for _, tag := range tc.want {
				if !strings.Contains(string(calls), "release create "+tag+" --title "+tag+" --verify-tag") {
					t.Errorf("no GitHub release for %s; gh calls:\n%s", tag, calls)
				}
			}
		})
	}
}

// multiImageVerifyStep returns the script of the step that makes sure
// CircleCI built every tag.
func multiImageVerifyStep(t *testing.T) string {
	t.Helper()

	rendered := renderInput(t, newWorkflows(t, gen.FlavourMultiImage).AutoReleaseMultiImage())
	var wf struct {
		Jobs map[string]struct {
			Steps []autoReleaseStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}
	for _, s := range wf.Jobs["tag"].Steps {
		if strings.HasPrefix(s.Name, "Verify CircleCI") {
			return s.Run
		}
	}

	t.Fatalf("no CircleCI verification step in the tag job:\n%s", rendered)
	return ""
}

// multiImageGhStub writes a gh answering `gh release view` with the first
// tag's publishedAt and `gh api` with $STUB_STATUSES, and returns the PATH
// entry.
func multiImageGhStub(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	gh := "#!/usr/bin/env bash\n" +
		"case \"$1\" in release) echo 2026-11-02T12:00:00Z ;; api) printf '%s\\n' \"$STUB_STATUSES\" ;; esac\n"
	for name, script := range map[string]string{"gh": gh, "sleep": "#!/usr/bin/env bash\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil { // #nosec G306 -- test-only executable stub
			t.Fatalf("write %s stub: %v", name, err)
		}
	}

	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// multiImageStatuses are the CircleCI statuses of the pushed commit: the
// branch build's setup, started before the release at 12:00:00Z, and one
// setup workflow started after it per tag built.
func multiImageStatuses(built int) string {
	statuses := []string{}
	for i := range built {
		statuses = append(statuses, `{"context":"ci/circleci: setup","state":"pending","created_at":"2026-11-02T12:00:0`+strconv.Itoa(i+3)+`Z","creator":{"login":"circleci-app[bot]"},"target_url":"https://app.circleci.com/workflow/tag`+strconv.Itoa(i)+`/job/j"}`)
	}
	statuses = append(statuses, `{"context":"ci/circleci: setup","state":"success","created_at":"2026-11-02T11:59:30Z","creator":{"login":"circleci-app[bot]"},"target_url":"https://app.circleci.com/workflow/branch/job/j"}`)
	return "[" + strings.Join(statuses, ",") + "]"
}

// Test_AutoReleaseMultiImageVerifiesCircleCI pins the tags' check: one setup
// workflow per tag started after the first release passes, fewer fail
// naming the fix, and a commit without any CircleCI status warns. The step
// holds no CircleCI credential and triggers nothing.
func Test_AutoReleaseMultiImageVerifiesCircleCI(t *testing.T) {
	tests := []struct {
		name     string
		statuses string
		tags     string
		fails    bool
		want     string
	}{
		{name: "every tag built", statuses: multiImageStatuses(2), want: "CircleCI builds the 2 tag(s)"},
		{name: "a lost tag fails", statuses: multiImageStatuses(1), fails: true, want: "::error::CircleCI started 1 build(s) for the 2 tag(s)"},
		{name: "no pipeline yet warns", statuses: "[]", want: "::warning::"},
		{name: "OAuth statuses alone warn, not verified", statuses: `[{"context":"ci/circleci: setup","state":"success","created_at":"2026-11-02T12:00:05Z","creator":{"login":"alice"},"target_url":"https://circleci.com/gh/giantswarm/agent-toolchains/9"}]`, want: "an OAuth follow builds"},
		{name: "a static configuration counts its App workflows", statuses: `[{"context":"ci/circleci: build","state":"pending","created_at":"2026-11-02T12:00:03Z","creator":{"login":"circleci-app[bot]"},"target_url":"https://app.circleci.com/workflow/s1/job/j"},{"context":"ci/circleci: build","state":"pending","created_at":"2026-11-02T12:00:04Z","creator":{"login":"circleci-app[bot]"},"target_url":"https://app.circleci.com/workflow/s2/job/j"}]`, want: "CircleCI builds the 2 tag(s)"},
		{name: "no tag cut on this push passes", tags: "\n", statuses: "[]", want: "no tag cut on this push"},
	}

	script := multiImageVerifyStep(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tags := tc.tags
			if tags == "" {
				tags = "a/v0.2.0\nb/v0.1.1\n"
			}
			writeRepoFile(t, filepath.Join(dir, "tags.txt"), tags)

			_, out, err := runStep(t, script, dir, multiImageGhStub(t),
				"GITHUB_REPOSITORY=giantswarm/agent-toolchains",
				"GITHUB_SHA=0123456789abcdef",
				"CIRCLECI_STATUS_WAIT_SECONDS=0",
				"STUB_STATUSES="+tc.statuses,
			)
			if (err != nil) != tc.fails {
				t.Fatalf("verify step error = %v, want failure %v\n%s", err, tc.fails, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}
