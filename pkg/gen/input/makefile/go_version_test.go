package makefile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"
)

// Exercise the generated recipes in real linked worktrees. The failing tool
// models older gitsemver releases, which accept a .git directory but not a file.
func TestGoVersionInWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the generated recipes require a POSIX shell")
	}
	for _, tool := range []string{"make", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}

	m, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	in := m.MakefileGenGo()[0]
	tpl, err := template.New("makefile").Parse(in.TemplateBody)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}

	const oldGitsemver = `#!/bin/sh
if [ ! -d .git ]; then
	echo 'error: finding git root from ".": path . is not inside git repository' >&2
	exit 1
fi
echo 1.2.3
`
	testCases := []struct {
		name      string
		tag       string
		annotated bool
		ahead     bool
		gitsemver string
		goal      string
		override  string
		build     bool
		want      string
		wantErr   bool
	}{
		{
			name: "case 0: build at a lightweight tag", tag: "v1.2.3",
			gitsemver: oldGitsemver, goal: "build", build: true, want: "1.2.3",
		},
		{
			name: "case 1: install at an annotated prerelease tag", tag: "v1.2.4-rc.1", annotated: true,
			gitsemver: oldGitsemver, goal: "install", build: true, want: "1.2.4-rc.1",
		},
		{
			name: "case 2: worktree ahead of a tag", tag: "v1.2.3", ahead: true,
			gitsemver: oldGitsemver, goal: "build", want: "1.2.3-1-g",
		},
		{
			name: "case 3: successful gitsemver stays authoritative", tag: "v1.2.3",
			gitsemver: "#!/bin/sh\necho 9.8.7-rc.2\n", goal: "build", want: "9.8.7-rc.2",
		},
		{
			name: "case 4: empty successful lookup uses the tag", tag: "v1.2.3",
			gitsemver: "#!/bin/sh\nexit 0\n", goal: "build", want: "1.2.3",
		},
		{
			name: "case 5: failed lookup discards partial stdout", tag: "v1.2.3",
			gitsemver: "#!/bin/sh\necho partial\nexit 1\n", goal: "build", want: "1.2.3",
		},
		{
			name:      "case 6: no tag still refuses a build",
			gitsemver: oldGitsemver, goal: "build", wantErr: true,
		},
		{
			name: "case 7: unrelated tags still refuse an install", tag: "release-not-a-version",
			gitsemver: oldGitsemver, goal: "install", wantErr: true,
		},
		{
			name:      "case 8: explicit override stays authoritative",
			gitsemver: oldGitsemver, goal: "build", override: "VERSION=4.5.6", want: "4.5.6",
		},
		{
			name: "case 9: explicitly empty override refuses a build", tag: "v1.2.3",
			gitsemver: oldGitsemver, goal: "build", override: "VERSION=", wantErr: true,
		},
		{
			name:      "case 10: lint tolerates an unresolved version",
			gitsemver: oldGitsemver, goal: "lint", want: "golangci-lint run",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			clone := filepath.Join(dir, "clone")
			if err := os.Mkdir(clone, 0o750); err != nil {
				t.Fatal(err)
			}
			write := func(name, content string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(filepath.Clean(name), []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...) // #nosec G204 -- test-owned git arguments and temporary repositories
				cmd.Dir = clone
				cmd.Env = append(cmd.Environ(),
					"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
					"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
					"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
				)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "--initial-branch=main")
			write(filepath.Join(clone, in.Path), rendered.String(), 0o600)
			write(filepath.Join(clone, "go.mod"), "module github.com/example/fixture\n\ngo 1.26.5\n", 0o600)
			write(filepath.Join(clone, "main.go"), "package main\nimport (\"fmt\"; \"github.com/example/fixture/pkg/project\")\nfunc main() { fmt.Println(project.Version()) }\n", 0o600)
			if err := os.MkdirAll(filepath.Join(clone, "pkg", "project"), 0o750); err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(clone, "pkg", "project", "project.go"), "package project\nvar version = \"dev\"\nfunc Version() string { return version }\n", 0o600)
			git("add", ".")
			git("commit", "-m", "fixture")
			if tc.tag != "" {
				if tc.annotated {
					git("tag", "-a", tc.tag, "-m", "fixture release")
				} else {
					git("tag", tc.tag)
				}
			}
			if tc.ahead {
				git("commit", "--allow-empty", "-m", "after tag")
			}
			worktree := filepath.Join(dir, "linked worktree")
			git("worktree", "add", "--detach", worktree, "HEAD")
			info, err := os.Stat(filepath.Join(worktree, ".git"))
			if err != nil || !info.Mode().IsRegular() {
				t.Fatalf("worktree .git must be a file: %v", err)
			}
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o750); err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(bin, "gitsemver"), tc.gitsemver, 0o700)
			args := []string{"--no-print-directory", "-f", in.Path, tc.goal,
				"APPLICATION=fixture", "MODULE=github.com/example/fixture", "OS=" + runtime.GOOS}
			if !tc.build {
				args = append(args, "-n")
			}
			if tc.override != "" {
				args = append(args, tc.override)
			}
			cmd := exec.Command("make", args...) // #nosec G204 -- generated recipes and arguments owned by this test
			cmd.Dir = worktree
			cmd.Env = append(cmd.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GOPATH="+filepath.Join(dir, "gopath"), "GOPROXY=off", "GOSUMDB=off", "GOWORK=off")
			out, err := cmd.CombinedOutput()
			if tc.wantErr {
				if err == nil || !strings.Contains(string(out), "VERSION is empty") {
					t.Fatalf("make %s: %v\n%s; want VERSION is empty", tc.goal, err, out)
				}
			} else {
				if err != nil {
					t.Fatalf("make %s: %v\n%s", tc.goal, err, out)
				}
				if strings.Contains(string(out), "finding git root") {
					t.Fatalf("handled lookup failure leaked to make output:\n%s", out)
				}
				want := tc.want
				if tc.ahead {
					want += git("rev-parse", "--short=7", "HEAD")
				}
				if tc.build {
					binary := filepath.Join(worktree, "fixture")
					if tc.goal == "install" {
						binary = filepath.Join(dir, "gopath", "bin", "fixture")
					}
					out, err = exec.Command(binary).CombinedOutput() // #nosec G204 -- the fixture binary just built in t.TempDir
					if err != nil || strings.TrimSpace(string(out)) != want {
						t.Fatalf("binary version = %q, %v; want %q", out, err, want)
					}
				} else if tc.goal == "build" {
					stamp := fmt.Sprintf("-X 'github.com/example/fixture/pkg/project.version=%s'", want)
					if !strings.Contains(string(out), stamp) {
						t.Fatalf("missing version stamp %q:\n%s", stamp, out)
					}
				} else if !strings.Contains(string(out), want) {
					t.Fatalf("make output = %q, want %q", out, want)
				}
			}
		})
	}
}
