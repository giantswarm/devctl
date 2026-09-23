// Package gitremote reads which GitHub repository a git checkout belongs to
// from its origin remote.
package gitremote

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

// Owner is the GitHub organization of the repositories devctl generates
// files for.
const Owner = "giantswarm"

// namePattern is the set of characters GitHub allows in a repository name.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Repo is the GitHub repository a remote URL points to.
type Repo struct {
	Host  string
	Owner string
	Name  string
}

// String names the repository as host/owner/name, without the credentials
// the remote URL may carry.
func (r Repo) String() string {
	return r.Host + "/" + r.Owner + "/" + r.Name
}

// RepoName returns the name of the giantswarm repository the git checkout in
// dir belongs to, read from its origin remote. It fails when the checkout has
// no origin remote or the remote is not a giantswarm repository. The name of
// the directory is never consulted: a worktree or a second clone is rarely
// named after its repository.
func RepoName(ctx context.Context, dir string) (string, error) {
	remote, err := OriginURL(ctx, dir)
	if err != nil {
		return "", err
	}

	repo, err := Parse(remote)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(repo.Owner, Owner) {
		return "", fmt.Errorf("the origin remote %s is not a %s repository", repo, Owner)
	}

	return repo.Name, nil
}

// OriginURL returns the URL of the origin remote of the git checkout in dir,
// as `git remote get-url origin` prints it.
func OriginURL(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("`git remote get-url origin` failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("`git remote get-url origin` failed: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// Parse returns the repository a git remote URL points to. It reads the
// <owner>/<name> path of a URL (https://github.com/giantswarm/devctl,
// ssh://git@github.com/giantswarm/devctl.git) and of git's scp-like syntax
// (git@github.com:giantswarm/devctl.git), a trailing .git stripped. The error
// never repeats the URL: an https remote may carry a token.
func Parse(remoteURL string) (Repo, error) {
	host, path, err := split(remoteURL)
	if err != nil {
		return Repo{}, err
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || !namePattern.MatchString(name) {
		return Repo{}, fmt.Errorf("the origin remote on %s names no <owner>/<name> repository", host)
	}

	return Repo{Host: host, Owner: owner, Name: name}, nil
}

// split separates a remote URL into its host and path.
func split(remoteURL string) (host, path string, err error) {
	if strings.Contains(remoteURL, "://") {
		u, err := url.Parse(remoteURL)
		// url.Parse's error quotes the URL, credentials included.
		if err != nil {
			return "", "", errors.New("the origin remote is not a URL git understands")
		}
		if u.Host == "" {
			return "", "", fmt.Errorf("the origin remote is the local path %s, not a GitHub repository", u.Path)
		}
		return u.Hostname(), u.Path, nil
	}

	// git reads [user@]host:path as scp-like syntax when the colon comes
	// before the first slash.
	if i := strings.Index(remoteURL, ":"); i > 0 && !strings.Contains(remoteURL[:i], "/") {
		host := remoteURL[:i]
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
		return host, remoteURL[i+1:], nil
	}

	return "", "", fmt.Errorf("the origin remote is the local path %s, not a GitHub repository", remoteURL)
}
