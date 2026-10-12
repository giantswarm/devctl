package gitops

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

func TestResolveOrder(t *testing.T) {
	t.Setenv(EnvTargetDir, "/umbrella")

	dir, _, err := (&Flags{Repo: DefaultRepo, Dir: "/flag"}).Resolve()
	if err != nil || dir != "/flag" {
		t.Errorf("flag: got %q, %v; want /flag", dir, err)
	}

	dir, _, err = (&Flags{Repo: DefaultRepo}).Resolve()
	if err != nil || dir != "/umbrella/cmc/giantswarm-management-clusters" {
		t.Errorf("env: got %q, %v", dir, err)
	}

	t.Setenv(EnvTargetDir, "")
	_, _, err = (&Flags{Repo: DefaultRepo}).Resolve()
	if !IsInvalidFlag(err) || !strings.Contains(err.Error(), "--"+FlagDir) || !strings.Contains(err.Error(), EnvTargetDir) {
		t.Errorf("neither: got %v, want an invalid-flag error naming both", err)
	}
}

func TestOpenRefusesAMissingDirectory(t *testing.T) {
	f := &Flags{Repo: DefaultRepo, Dir: filepath.Join(t.TempDir(), "nope")}
	_, err := f.Open(context.Background(), &bytes.Buffer{})
	if !IsInvalidFlag(err) || !strings.Contains(err.Error(), "--"+FlagDir) {
		t.Errorf("got %v, want an invalid-flag error naming --%s", err, FlagDir)
	}
}

// newPair returns a checkout of a local bare origin, with one commit pushed,
// and a second clone to push further commits from.
func newPair(t *testing.T) (dir, other string) {
	t.Helper()

	origin := t.TempDir()
	gittest.RunGit(t, origin, "init", "--bare", "-b", "main")
	dir = t.TempDir()
	gittest.RunGit(t, dir, "init", "-b", "main")
	gittest.RunGit(t, dir, "remote", "add", "origin", origin)
	commit(t, dir, "a")
	gittest.RunGit(t, dir, "push", "-u", "origin", "main")

	other = t.TempDir()
	gittest.RunGit(t, other, "clone", origin, ".")

	return dir, other
}

func commit(t *testing.T, dir, file string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.RunGit(t, dir, "add", "-A")
	gittest.RunGit(t, dir, "commit", "-m", file)
}

func TestSync(t *testing.T) {
	ctx := context.Background()

	t.Run("equal", func(t *testing.T) {
		dir, _ := newPair(t)
		var stderr bytes.Buffer
		if err := Sync(ctx, dir, &stderr); err != nil || stderr.Len() != 0 {
			t.Errorf("got %v, %q; want quiet success", err, stderr.String())
		}
	})

	t.Run("behind fast-forwards", func(t *testing.T) {
		dir, other := newPair(t)
		commit(t, other, "b")
		gittest.RunGit(t, other, "push", "origin", "main")

		var stderr bytes.Buffer
		if err := Sync(ctx, dir, &stderr); err != nil {
			t.Fatal(err)
		}
		if got, want := gittest.GitOutput(t, dir, "rev-parse", "HEAD"), gittest.GitOutput(t, other, "rev-parse", "HEAD"); got != want {
			t.Errorf("HEAD %s, want %s", got, want)
		}
		if !strings.Contains(stderr.String(), "Updated "+dir+" to origin/main at ") {
			t.Errorf("stderr %q does not say it updated", stderr.String())
		}
	})

	t.Run("shallow behind fast-forwards", func(t *testing.T) {
		_, other := newPair(t)
		origin := gittest.GitOutput(t, other, "remote", "get-url", "origin")
		shallow := t.TempDir()
		gittest.RunGit(t, shallow, "clone", "--depth", "1", "file://"+origin, ".")
		commit(t, other, "b")
		gittest.RunGit(t, other, "push", "origin", "main")

		if err := Sync(ctx, shallow, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if got, want := gittest.GitOutput(t, shallow, "rev-parse", "HEAD"), gittest.GitOutput(t, other, "rev-parse", "HEAD"); got != want {
			t.Errorf("HEAD %s, want %s", got, want)
		}
	})

	t.Run("dirty stops", func(t *testing.T) {
		dir, _ := newPair(t)
		if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := Sync(ctx, dir, &bytes.Buffer{})
		if !IsSync(err) || !strings.Contains(err.Error(), "is dirty") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("off main stops", func(t *testing.T) {
		dir, _ := newPair(t)
		gittest.RunGit(t, dir, "checkout", "-b", "feature")
		err := Sync(ctx, dir, &bytes.Buffer{})
		if !IsSync(err) || !strings.Contains(err.Error(), "is on branch feature, not main; devctl cannot safely sync the GitOps repo") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("ahead stops with the reset command", func(t *testing.T) {
		dir, _ := newPair(t)
		commit(t, dir, "local")
		err := Sync(ctx, dir, &bytes.Buffer{})
		if !IsSync(err) || !strings.Contains(err.Error(), "1 local commit(s)") || !strings.Contains(err.Error(), "git -C "+dir+" reset --hard origin/main") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("diverged stops", func(t *testing.T) {
		dir, other := newPair(t)
		commit(t, other, "b")
		gittest.RunGit(t, other, "push", "origin", "main")
		commit(t, dir, "local")
		err := Sync(ctx, dir, &bytes.Buffer{})
		if !IsSync(err) || !strings.Contains(err.Error(), "reset --hard origin/main") {
			t.Errorf("got %v", err)
		}
	})
}
