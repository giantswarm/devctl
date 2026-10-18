package changelog

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
)

// Releases reads the releases reachable from HEAD in the git repository at
// dir: its "v"-prefixed semver tags, the tags on one commit as one release, and
// each tag's file at path (CHANGELOG.md), relative to dir.
func Releases(dir, path string) ([]Release, error) {
	// An annotated tag's commit and its date are the starred fields; a
	// lightweight tag's own object is the commit.
	out, err := git(dir, nil, "for-each-ref", "--merged", "HEAD",
		"--format=%(refname:short)%09%(objectname)%09%(committerdate:short)%09%(*objectname)%09%(*committerdate:short)",
		"refs/tags/v*")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	byCommit := map[string]*Release{}
	var commits []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			continue
		}
		tag, commit, date := fields[0], fields[1], fields[2]
		if fields[3] != "" {
			commit, date = fields[3], fields[4]
		}
		v, err := semver.StrictNewVersion(strings.TrimPrefix(tag, "v"))
		if err != nil {
			continue
		}
		if r, ok := byCommit[commit]; ok {
			if v.GreaterThan(r.Version) {
				r.Version, r.Tag = v, tag
			}
			continue
		}
		byCommit[commit] = &Release{Version: v, Tag: tag, Date: date}
		commits = append(commits, commit)
	}

	files, err := blobs(dir, commits, path)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	releases := make([]Release, 0, len(commits))
	for _, c := range commits {
		r := byCommit[c]
		r.Changelog = files[c]
		releases = append(releases, *r)
	}
	return sorted(releases), nil
}

// File reads the file at path, relative to dir, at the revision rev of the git
// repository at dir; "" when rev has no such file.
func File(dir, rev, path string) (string, error) {
	commit, err := git(dir, nil, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", microerror.Maskf(executionFailedError, "revision %q: no such commit in the clone", rev)
	}
	commit = strings.TrimSpace(commit)
	files, err := blobs(dir, []string{commit}, path)
	if err != nil {
		return "", microerror.Mask(err)
	}
	return files[commit], nil
}

// blobs reads the file at path in every commit through one `git cat-file
// --batch`; a commit without the file maps to "".
func blobs(dir string, commits []string, path string) (map[string]string, error) {
	var in bytes.Buffer
	for _, c := range commits {
		// "./" resolves the path from dir, not from the repository root.
		fmt.Fprintf(&in, "%s:./%s\n", c, filepath.ToSlash(path))
	}
	out, err := git(dir, &in, "cat-file", "--batch")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	files := map[string]string{}
	rd := bufio.NewReader(strings.NewReader(out))
	for _, c := range commits {
		header, err := rd.ReadString('\n')
		if err != nil {
			return nil, microerror.Maskf(executionFailedError, "git cat-file: reading %s:%s: %v", c, path, err)
		}
		// "<object> missing" for a commit without the file, else
		// "<object> blob <size>".
		fields := strings.Fields(header)
		if len(fields) != 3 {
			continue
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, microerror.Maskf(executionFailedError, "git cat-file: header %q", strings.TrimSpace(header))
		}
		buf := make([]byte, size+1) // the content and its trailing newline
		if _, err := io.ReadFull(rd, buf); err != nil {
			return nil, microerror.Maskf(executionFailedError, "git cat-file: reading %s:%s: %v", c, path, err)
		}
		if fields[1] == "blob" {
			files[c] = string(buf[:size])
		}
	}
	return files, nil
}

func git(dir string, stdin io.Reader, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", args...) //nolint:gosec // git with this package's own arguments
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", microerror.Maskf(executionFailedError, "git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
