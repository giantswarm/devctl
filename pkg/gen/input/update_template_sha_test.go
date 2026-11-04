package input

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateTemplateSHA(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("template provenance generation requires git")
	}
	run := func(t *testing.T, dir, name string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	binary := filepath.Join(t.TempDir(), "update-template-sha")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	run(t, ".", "go", "build", "-p", "2", "-o", binary, "update-template-sha.go")

	root := t.TempDir()
	repo := filepath.Join(root, "physical checkout")
	const templateDir = "pkg/gen/input/example/internal/file"
	dir := filepath.Join(repo, filepath.FromSlash(templateDir))
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	writeTemplate := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "x.template"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(message string) string {
		t.Helper()
		run(t, repo, "git", "add", "--", templateDir+"/x.template")
		run(t, repo, "git", "-c", "user.name=Synthetic Audit", "-c", "user.email=audit@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", message)
		return run(t, repo, "git", "rev-parse", "HEAD")
	}
	run(t, repo, "git", "init", "-q")
	run(t, repo, "git", "config", "core.hooksPath", filepath.Join(root, "no-hooks"))
	run(t, repo, "git", "symbolic-ref", "HEAD", "refs/heads/audit-main")
	t.Setenv("GIT_AUTHOR_DATE", "2000-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2000-01-01T00:00:00Z")
	writeTemplate("original template\n")
	sha := commit("add template")
	// A later commit on another ref must not supply the provenance of HEAD.
	run(t, repo, "git", "checkout", "-qb", "unmerged-template")
	t.Setenv("GIT_AUTHOR_DATE", "2000-01-02T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2000-01-02T00:00:00Z")
	writeTemplate("unmerged template\n")
	commit("change template on another branch")
	run(t, repo, "git", "checkout", "-q", "audit-main")
	commit("unrelated main commit")
	want := "https://github.com/giantswarm/devctl/blob/" + sha + "/" + templateDir + "/x.template"
	check := func(t *testing.T, workdir string) {
		t.Helper()
		run(t, workdir, binary, "x.template")
		got, err := os.ReadFile(filepath.Join(dir, "x.template.sha"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("provenance = %q, want %q", got, want)
		}
	}
	t.Run("physical path", func(t *testing.T) {
		check(t, dir)
	})
	t.Run("symlink path", func(t *testing.T) {
		link := filepath.Join(root, "linked checkout")
		if err := os.Symlink(repo, link); err != nil {
			t.Skipf("directory symlinks unavailable: %v", err)
		}
		check(t, filepath.Join(link, filepath.FromSlash(templateDir)))
	})
}
