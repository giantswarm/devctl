package circleci

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	repoAgentToolchains = "agent-toolchains"

	goldenMultiImageSetupPath     = "testdata/agent-toolchains.config.yml"
	goldenMultiImageWorkflowsPath = "testdata/agent-toolchains.workflows.yml"
)

func multiImageConfig() Config {
	return Config{
		RepoName: repoAgentToolchains,
		Language: gen.LanguageGeneric,
		Flavours: gen.FlavourSlice{gen.FlavourMultiImage},
	}
}

// Test_GoldenMultiImage pins both files of the multi-image pipeline.
func Test_GoldenMultiImage(t *testing.T) {
	c := newCircleCI(t, multiImageConfig())

	assertGolden(t, goldenMultiImageSetupPath, renderInput(t, c.SetupConfig()))
	assertGolden(t, goldenMultiImageWorkflowsPath, renderInput(t, c.Workflows()))
}

// Test_MultiImageNeedsNoLanguageSignal pins that a multi-image repository
// renders without the signals the single-image pipeline needs (a language
// job, a root Dockerfile, a chart): its images are the build.
func Test_MultiImageNeedsNoLanguageSignal(t *testing.T) {
	c := multiImageConfig()
	c.Language = gen.Language("")

	if _, err := New(c); err != nil {
		t.Fatalf("New() = %v, want a multi-image pipeline without a language", err)
	}
}

func Test_MultiImageOptions(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(*Config)
		want  []string
		never []string
	}{
		{
			name:  "public by default",
			edit:  func(*Config) {},
			want:  []string{"public gsoci.azurecr.io ACR_GSOCI_USERNAME ACR_GSOCI_PASSWORD true", `platforms: "linux/amd64,linux/arm64"`, "image: giantswarm/agent-toolchains/__IMAGE__"},
			never: []string{"force-public", "gsociprivate"},
		},
		{
			name:  "a private repository's public images",
			edit:  func(c *Config) { c.ForcePublic = true },
			want:  []string{"force-public: true", "public gsoci.azurecr.io"},
			never: []string{"gsociprivate"},
		},
		{
			name:  "private images",
			edit:  func(c *Config) { c.ImagePrivateOnly = true },
			want:  []string{"private gsociprivate.azurecr.io ACR_GSOCIPRIVATE_USERNAME ACR_GSOCIPRIVATE_PASSWORD true"},
			never: []string{"force-public", "public gsoci.azurecr.io"},
		},
		{
			name: "image prefix and platforms",
			edit: func(c *Config) {
				c.ImageName = "giantswarm/toolchains"
				c.ImagePlatforms = "linux/arm64"
			},
			want:  []string{"image: giantswarm/toolchains/__IMAGE__", `platforms: "linux/arm64"`},
			never: []string{"giantswarm/agent-toolchains/"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := multiImageConfig()
			tc.edit(&c)
			got := render(t, c)
			for _, s := range tc.want {
				if !contains(got, s) {
					t.Errorf("workflows.yml lacks %q:\n%s", s, got)
				}
			}
			for _, s := range tc.never {
				if contains(got, s) {
					t.Errorf("workflows.yml carries %q:\n%s", s, got)
				}
			}
		})
	}
}

func Test_MultiImageRejects(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"app flavour", func(c *Config) { c.Flavours = append(c.Flavours, gen.FlavourApp) }},
		{"cli flavour", func(c *Config) { c.Flavours = append(c.Flavours, gen.FlavourCLI) }},
		{"public and private", func(c *Config) { c.ForcePublic, c.ImagePrivateOnly = true, true }},
		{"a Dockerfile path", func(c *Config) { c.ImageDockerfile = "Dockerfile" }},
		{"native builds", func(c *Config) { c.ImageNativeBuilds = true }},
		{"a pre-build job", func(c *Config) { c.ImagePreBuildJob = "prepare" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := multiImageConfig()
			tc.edit(&c)
			if _, err := New(c); !IsInvalidConfig(err) {
				t.Fatalf("New() = %v, want an invalid config error", err)
			}
		})
	}
}

// multiImageRepo is a checkout of a multi-image repository with an origin
// whose main carries it: the images a, b and c, the shared build files under
// toolchains/lib, and the generated .circleci files.
func multiImageRepo(t *testing.T, c Config) string {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir := filepath.Join(root, "repo")
	mustRun(t, root, "git", "init", "-q", "--bare", "--initial-branch=main", origin)
	mustRun(t, root, "git", "clone", "-q", origin, dir)

	in := newCircleCI(t, c)
	files := map[string]string{
		".circleci/config.yml":      renderInput(t, in.SetupConfig()),
		".circleci/workflows.yml":   renderInput(t, in.Workflows()),
		"README.md":                 "repo\n",
		"toolchains/a/Dockerfile":   "FROM scratch\n",
		"toolchains/b/Dockerfile":   "FROM scratch\n",
		"toolchains/c/Dockerfile":   "FROM scratch\n",
		"toolchains/lib/fetch.sh":   "true\n",
		"toolchains/notes/plain.md": "not an image\n",
	}
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
	}
	commitAll(t, dir, "chore: initial")
	mustRun(t, dir, "git", "push", "-q", "origin", "HEAD:main")

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G703 -- t.TempDir() path, test-only
		t.Fatalf("write %s: %v", path, err)
	}
}

func commitAll(t *testing.T, dir, subject string) {
	t.Helper()

	mustRun(t, dir, "git", "add", "-A")
	mustRun(t, dir, "git", "commit", "-q", "--allow-empty", "-m", subject)
}

// mustRun runs a command with the test's git identity and no user or system
// git configuration (no signing key, no hooks).
func mustRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), name, args...) // #nosec G204 -- args are built by this test, test-only
	cmd.Dir = dir
	cmd.Env = gitTestEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}

	return string(out)
}

func gitTestEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
}

// runSetup runs the render step of the generated setup config in dir, as the
// setup job does, with the mikefarah yq first on PATH, and returns the
// continuation config, what the step printed and its exit error.
func runSetup(t *testing.T, dir string, env ...string) (string, string, error) {
	t.Helper()

	yq := findYq(t)
	bin := t.TempDir()
	if err := os.Symlink(yq, filepath.Join(bin, "yq")); err != nil {
		t.Fatalf("link yq: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".circleci/config.yml")) // #nosec G304 -- t.TempDir() path, test-only
	if err != nil {
		t.Fatalf("read config.yml: %v", err)
	}
	var setup struct {
		Jobs struct {
			Setup struct {
				Steps []yaml.Node `yaml:"steps"`
			} `yaml:"setup"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &setup); err != nil {
		t.Fatalf("config.yml is not valid YAML: %v", err)
	}
	var step struct {
		Run struct {
			Environment map[string]string `yaml:"environment"`
			Command     string            `yaml:"command"`
		} `yaml:"run"`
	}
	for _, node := range setup.Jobs.Setup.Steps {
		if err := node.Decode(&step); err == nil && step.Run.Command != "" {
			break
		}
	}
	if step.Run.Command == "" {
		t.Fatalf("the setup job has no run step:\n%s", raw)
	}

	cmd := exec.CommandContext(t.Context(), "bash", "-c", step.Run.Command) // #nosec G204 -- the generated script under test, test-only
	cmd.Dir = dir
	cmd.Env = append(gitTestEnv(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CIRCLE_TAG=")
	for k, v := range step.Run.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, env...)
	out, runErr := cmd.CombinedOutput()

	continued, _ := os.ReadFile(filepath.Join(dir, ".circleci/continue.yml")) // #nosec G304 -- t.TempDir() path, test-only
	return string(continued), string(out), runErr
}

// continuation is the part of a continuation config the tests read.
type continuation struct {
	Jobs      map[string]any `yaml:"jobs"`
	Workflows map[string]struct {
		Jobs []any `yaml:"jobs"`
	} `yaml:"workflows"`
}

func parseContinuation(t *testing.T, doc string) continuation {
	t.Helper()

	var c continuation
	if err := yaml.Unmarshal([]byte(doc), &c); err != nil {
		t.Fatalf("continuation is not valid YAML: %v\n%s", err, doc)
	}

	return c
}

func workflowNames(c continuation) []string {
	var names []string
	for name := range c.Workflows {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

const checkImageJob = `jobs:
  check-image:
    parameters:
      image:
        type: string
    docker:
    - image: cimg/base:current
    steps:
    - checkout
    - run: echo << parameters.image >>
`

// Test_MultiImageSetupPicksTheImages runs the generated setup step on real
// git histories: which per-image workflows a branch or a tag continues with.
func Test_MultiImageSetupPicksTheImages(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, dir string)
		env    []string
		want   []string
	}{
		{
			name: "a branch builds the image it changed",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "toolchains/b/Dockerfile"), "FROM scratch\nLABEL x=y\n")
			},
			want: []string{"build-b"},
		},
		{
			name: "a branch that changes the shared build files builds every image",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "toolchains/lib/fetch.sh"), "false\n")
			},
			want: []string{"build-a", "build-b", "build-c"},
		},
		{
			name: "a branch that adds an image builds it",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "toolchains/new-one/Dockerfile"), "FROM scratch\n")
			},
			want: []string{"build-new-one"},
		},
		{
			name: "a branch that removes an image builds nothing",
			change: func(t *testing.T, dir string) {
				mustRun(t, dir, "git", "rm", "-q", "-r", "toolchains/c")
			},
			want: []string{"no-op"},
		},
		{
			name: "a branch that removes the shared build files builds every image",
			change: func(t *testing.T, dir string) {
				mustRun(t, dir, "git", "rm", "-q", "-r", "toolchains/lib")
			},
			want: []string{"build-a", "build-b", "build-c"},
		},
		{
			name: "a branch outside the images builds nothing",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "README.md"), "changed\n")
			},
			want: []string{"no-op"},
		},
		{
			name: "the default branch builds nothing",
			want: []string{"no-op"},
		},
		{
			name: "a tag releases its image",
			env:  []string{"CIRCLE_TAG=a/v1.2.3"},
			want: []string{"release-a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := multiImageRepo(t, multiImageConfig())
			if tc.change != nil {
				mustRun(t, dir, "git", "checkout", "-q", "-b", "change")
				tc.change(t, dir)
				commitAll(t, dir, "feat: change")
			}

			doc, out, err := runSetup(t, dir, tc.env...)
			if err != nil {
				t.Fatalf("setup step failed: %v\n%s", err, out)
			}
			got := workflowNames(parseContinuation(t, doc))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("workflows = %v, want %v\n%s", got, tc.want, out)
			}
			for line := range strings.Lines(doc) {
				if !strings.HasPrefix(strings.TrimSpace(line), "#") && strings.Contains(line, "__IMAGE__") {
					t.Errorf("continuation still carries the placeholder: %s", line)
				}
			}
		})
	}
}

// Test_MultiImageSetupRefusesAForeignTag pins that a tag naming no image
// fails the pipeline instead of publishing nothing in silence.
func Test_MultiImageSetupRefusesAForeignTag(t *testing.T) {
	for _, tag := range []string{"v1.0.0", "z/v1.0.0", "notes/v1.0.0", "lib/v1.0.0"} {
		t.Run(tag, func(t *testing.T) {
			dir := multiImageRepo(t, multiImageConfig())
			if _, out, err := runSetup(t, dir, "CIRCLE_TAG="+tag); err == nil || !strings.Contains(out, "names no image") {
				t.Fatalf("setup step = %v, want a refusal naming the tag\n%s", err, out)
			}
		})
	}
}

// Test_MultiImageReleaseCheck pins the repository's check: the release push
// requires check-image when custom.yml defines it, and publishes straight
// away when it does not, with custom.yml merged in either way.
func Test_MultiImageReleaseCheck(t *testing.T) {
	for _, withCheck := range []bool{true, false} {
		name := "without check-image"
		if withCheck {
			name = "with check-image"
		}
		t.Run(name, func(t *testing.T) {
			dir := multiImageRepo(t, multiImageConfig())
			custom := "workflows:\n  nightly:\n    jobs:\n    - nightly\njobs:\n  nightly:\n    docker:\n    - image: cimg/base:current\n    steps:\n    - run: true\n"
			if withCheck {
				custom = checkImageJob + "  nightly:\n    docker:\n    - image: cimg/base:current\n    steps:\n    - run: true\nworkflows:\n  nightly:\n    jobs:\n    - nightly\n"
			}
			writeFile(t, filepath.Join(dir, CustomConfigPath), custom)
			commitAll(t, dir, "chore: custom")

			doc, out, err := runSetup(t, dir, "CIRCLE_TAG=b/v0.2.0")
			if err != nil {
				t.Fatalf("setup step failed: %v\n%s", err, out)
			}
			c := parseContinuation(t, doc)
			if got := workflowNames(c); strings.Join(got, ",") != "nightly,release-b" {
				t.Fatalf("workflows = %v, want the release and custom.yml's own\n%s", got, doc)
			}

			var check, push map[string]any
			for _, entry := range c.Workflows["release-b"].Jobs {
				job, _ := entry.(map[string]any)
				if p, ok := job["check-image"].(map[string]any); ok {
					check = p
				}
				if p, ok := job[PushToRegistriesJob].(map[string]any); ok {
					push = p
				}
			}
			if push == nil {
				t.Fatalf("release-b has no push job:\n%s", doc)
			}
			if push["git-tag-prefix"] != "b" || push["image"] != "giantswarm/agent-toolchains/b" || push["dockerfile"] != "./toolchains/b/Dockerfile" {
				t.Errorf("push job = %v, want image b from its own directory and tag line", push)
			}
			if withCheck {
				if check == nil || check["image"] != "b" || check["name"] != "check-image-b" {
					t.Errorf("check job = %v, want check-image of b", check)
				}
				if requires, _ := push["requires"].([]any); len(requires) != 1 || requires[0] != "check-image-b" {
					t.Errorf("push requires %v, want check-image-b", push["requires"])
				}
			} else if check != nil || push["requires"] != nil {
				t.Errorf("without check-image the release carries check %v and requires %v:\n%s", check, push["requires"], doc)
			}
		})
	}
}
