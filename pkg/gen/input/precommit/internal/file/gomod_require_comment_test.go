package file

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"regexp"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// goModRequireCommentCases are go.mod lines and whether the go-mod-require-comment hook
// refuses them: a require line (single or in a block) whose trailing comment has two or
// more words, the shape Renovate's gomod manager skips.
var goModRequireCommentCases = map[string]bool{
	"require github.com/google/certificate-transparency-go v1.3.2 // indirect; master until the release after v1.3.3": true,
	"\tgithub.com/google/certificate-transparency-go v1.3.2 // indirect; master until the release after v1.3.3":       true,
	"\tgithub.com/foo/bar v1.2.3 // pinned until upstream fixes the regression":                                       true,
	"\tgithub.com/foo/bar v0.0.0-20260101000000-abcdef123456 // see the comment above":                                true,
	"\tgopkg.in/yaml.v3 v3.0.1 //indirect but needed":                                                                 true,
	"\tgithub.com/foo/bar v2.0.0+incompatible // two words":                                                           true,
	"require example.com/foo v1.0.0 // two words":                                                                     true,

	"require github.com/foo/bar v1.2.3":              false,
	"require github.com/foo/bar v1.2.3 // indirect":  false,
	"\tgithub.com/foo/bar v1.2.3":                    false,
	"\tgithub.com/foo/bar v1.2.3 // indirect":        false,
	"\tgithub.com/foo/bar v1.2.3 // indirect ":       false,
	"\tgithub.com/foo/bar v1.2.3 //indirect":         false,
	"\tgithub.com/foo/bar v1.2.3 // pinned":          false,
	"\tgithub.com/foo/bar v1.2.3 // indirect;pinned": false,
	"require (": false,
	")":         false,
	"// pinned until upstream v1.2.4 ships the fix":                               false,
	"\t// pinned until upstream v1.2.4 ships the fix":                             false,
	"module github.com/giantswarm/my-repo // two words":                           false,
	"go 1.25.0 // two words":                                                      false,
	"toolchain go1.25.1 // two words":                                             false,
	"tool golang.org/x/tools/cmd/stringer // two words":                           false,
	"godebug default=go1.21 // two words":                                         false,
	"retract v1.0.0 // published by mistake":                                      false,
	"retract [v1.0.0, v1.0.5] // published by mistake":                            false,
	"\tv1.0.0 // published by mistake":                                            false,
	"\t[v1.0.0, v1.0.5] // published by mistake":                                  false,
	"replace github.com/foo/bar => ../bar // local checkout":                      false,
	"replace github.com/foo/bar v1.2.3 => github.com/fork/bar v1.2.4 // our fork": false,
	"\tgithub.com/foo/bar => github.com/fork/bar v1.2.4 // our fork":              false,
	"\tgithub.com/foo/bar v1.2.3 => github.com/fork/bar v1.2.4 // our fork":       false,
}

// renderedGoModRequireCommentHook renders the Go .pre-commit-config.yaml and returns its
// go-mod-require-comment hook, or false when the config has none.
func renderedGoModRequireCommentHook(t *testing.T, language string) (goModRequireCommentHook, bool) {
	t.Helper()

	in := NewCreatePreCommitConfigInput(params.Params{Language: language, RepoName: "my-repo"})
	tpl, err := template.New(in.Path).Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	var config struct {
		Repos []struct {
			Repo  string                    `yaml:"repo"`
			Hooks []goModRequireCommentHook `yaml:"hooks"`
		} `yaml:"repos"`
	}
	if err := yaml.Unmarshal(rendered.Bytes(), &config); err != nil {
		t.Fatalf("unmarshal .pre-commit-config.yaml: %v\n%s", err, rendered.String())
	}
	for _, repo := range config.Repos {
		for _, hook := range repo.Hooks {
			if hook.ID == "go-mod-require-comment" {
				if repo.Repo != "local" {
					t.Errorf("go-mod-require-comment: repo %q, want local", repo.Repo)
				}
				return hook, true
			}
		}
	}
	return goModRequireCommentHook{}, false
}

type goModRequireCommentHook struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Language string `yaml:"language"`
	Files    string `yaml:"files"`
	Entry    string `yaml:"entry"`
}

// Test_GoModRequireCommentHook checks that a Go repository's config carries the hook as a
// pygrep hook on every go.mod, root and nested, with the exact pattern under test, and
// that a non-Go repository's config does not.
func Test_GoModRequireCommentHook(t *testing.T) {
	hook, ok := renderedGoModRequireCommentHook(t, "go")
	if !ok {
		t.Fatal("no go-mod-require-comment hook in the Go config")
	}
	if hook.Language != "pygrep" {
		t.Errorf("language: got %q, want pygrep", hook.Language)
	}
	if hook.Entry != goModRequireCommentRegex {
		t.Errorf("entry: got %q, want %q", hook.Entry, goModRequireCommentRegex)
	}
	files, err := regexp.Compile(hook.Files)
	if err != nil {
		t.Fatalf("compile files %q: %v", hook.Files, err)
	}
	for path, want := range map[string]bool{
		"go.mod":              true,
		"tools/go.mod":        true,
		"api/v2/go.mod":       true,
		"go.sum":              false,
		"notgo.mod":           false,
		"testdata/go.mod.txt": false,
	} {
		if got := files.MatchString(path); got != want {
			t.Errorf("files %q matches %s: got %v, want %v", hook.Files, path, got, want)
		}
	}

	if _, ok := renderedGoModRequireCommentHook(t, "generic"); ok {
		t.Error("go-mod-require-comment hook in the generic config")
	}
}

// Test_GoModRequireCommentRegex runs the hook's pattern over goModRequireCommentCases with
// Go's regexp.
func Test_GoModRequireCommentRegex(t *testing.T) {
	re := regexp.MustCompile(goModRequireCommentRegex)
	for line, want := range goModRequireCommentCases {
		if got := re.MatchString(line); got != want {
			t.Errorf("%q: refused %v, want %v", line, got, want)
		}
	}
}

// Test_GoModRequireCommentRegexPython runs the same cases through Python's re the way
// pre-commit's pygrep does (re.search on each line, as bytes), the engine the generated
// hook actually runs on.
func Test_GoModRequireCommentRegexPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	lines := make([]string, 0, len(goModRequireCommentCases))
	for line := range goModRequireCommentCases {
		lines = append(lines, line)
	}
	in, err := json.Marshal(lines)
	if err != nil {
		t.Fatalf("marshal cases: %v", err)
	}

	const program = `import json,re,sys; p=re.compile(sys.argv[1].encode()); print(json.dumps([p.search(l.encode()) is not None for l in json.load(sys.stdin)]))`
	cmd := exec.Command(python, "-c", program, goModRequireCommentRegex) // #nosec G204 -- interpreter, program and pattern are fixed, test-only
	cmd.Stdin = bytes.NewReader(in)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("python3: %v", err)
	}
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if len(got) != len(lines) {
		t.Fatalf("python3 returned %d results for %d lines", len(got), len(lines))
	}
	for i, line := range lines {
		if want := goModRequireCommentCases[line]; got[i] != want {
			t.Errorf("%q: refused %v, want %v", line, got[i], want)
		}
	}
}
