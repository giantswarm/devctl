package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Checkouts give a reservation call a checkout of its GitOps repository of
// its own, as the person.
type Checkouts interface {
	// Clone checks out main of repository, owner/repo, into a new directory,
	// fetching and pushing with token and committing as author. remove
	// deletes the directory; the call runs it when it ends.
	Clone(ctx context.Context, repository, token string, author Caller) (dir string, remove func(), err error)
}

// DefaultGitBaseURL is where GitCheckouts clone from.
const DefaultGitBaseURL = "https://github.com"

// GitCheckouts clone with git: a shallow clone of main from BaseURL into a
// new directory under Dir. The person's token is the clone's credential, in
// its own git configuration only, never on a command line; the author is the
// person's GitHub login and noreply address.
type GitCheckouts struct {
	// BaseURL is the git host; empty is DefaultGitBaseURL.
	BaseURL string
	// Dir holds the clones; empty is the system's temporary directory.
	Dir string
}

// Clone implements [Checkouts].
func (g GitCheckouts) Clone(ctx context.Context, repository, token string, author Caller) (string, func(), error) {
	dir, err := os.MkdirTemp(g.Dir, "reservation-")
	if err != nil {
		return "", nil, fmt.Errorf("creating the checkout of %s: %w", repository, err)
	}
	remove := func() { _ = os.RemoveAll(dir) }
	if err := g.clone(ctx, dir, repository, token, author); err != nil {
		remove()
		return "", nil, err
	}
	return dir, remove, nil
}

func (g GitCheckouts) clone(ctx context.Context, dir, repository, token string, author Caller) error {
	base := strings.TrimSuffix(g.BaseURL, "/")
	if base == "" {
		base = DefaultGitBaseURL
	}
	if _, err := gitIn(ctx, dir, "init", "--quiet", "--initial-branch=main"); err != nil {
		return err
	}
	config := fmt.Sprintf("[remote \"origin\"]\n\turl = %s/%s.git\n[user]\n\tname = %s\n\temail = %s\n[commit]\n\tgpgsign = false\n",
		base, repository, author.Login, NoreplyEmail(author))
	if token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		config += fmt.Sprintf("[http \"%s/\"]\n\textraHeader = Authorization: Basic %s\n", base, basic)
	}
	if err := appendFile(filepath.Join(dir, ".git", "config"), config); err != nil {
		return fmt.Errorf("configuring the checkout of %s: %w", repository, err)
	}
	if _, err := gitIn(ctx, dir, "fetch", "--quiet", "--depth=1", "origin", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
		return fmt.Errorf("cloning %s: %w", repository, err)
	}
	if _, err := gitIn(ctx, dir, "reset", "--quiet", "--hard", "origin/main"); err != nil {
		return fmt.Errorf("checking out main of %s: %w", repository, err)
	}
	return nil
}

// NoreplyEmail is the person's GitHub noreply address, the commits' author
// email: <id>+<login>@users.noreply.github.com.
func NoreplyEmail(c Caller) string {
	if c.ID == 0 {
		return c.Login + "@users.noreply.github.com"
	}
	return fmt.Sprintf("%d+%s@users.noreply.github.com", c.ID, c.Login)
}

func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the checkout's own git config
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// gitIn runs git in dir, never prompting, and returns its trimmed stdout; an
// error carries its stderr.
func gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // fixed arguments
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
