package workflows

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// RemoveChangelogUnreleasedSection rewrites the file at path in place, dropping
// the "## [Unreleased]" section (heading line plus content) up to but not
// including the next "## " heading. No-op when the file does not exist or does
// not contain an "## [Unreleased]" heading.
//
// Used after a repo opts into release-please: that flow is commit-driven, so
// the "[Unreleased]" section is no longer the curation point and would
// otherwise be stranded mid-file by release-please's next-version inserts
// (which land directly above the most recent "## " heading).
func RemoveChangelogUnreleasedSection(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Detect a trailing newline so we can preserve the file's terminator after
	// rewriting. strings.Split on "\n" turns "a\nb\n" into ["a","b",""] — the
	// trailing empty slot lets strings.Join produce the same terminator.
	hadTrailingNewline := strings.HasSuffix(string(data), "\n")
	lines := strings.Split(string(data), "\n")

	out := make([]string, 0, len(lines))
	inUnreleased := false
	changed := false

	for _, line := range lines {
		if !inUnreleased && isUnreleasedHeading(line) {
			inUnreleased = true
			changed = true
			continue
		}
		if inUnreleased {
			if strings.HasPrefix(line, "## ") {
				// Reached the next H2 heading — end of the Unreleased section.
				inUnreleased = false
				out = append(out, line)
			}
			// Otherwise we're inside the section — drop the line.
			continue
		}
		out = append(out, line)
	}

	if !changed {
		return nil
	}

	rewritten := strings.Join(out, "\n")
	if !hadTrailingNewline && strings.HasSuffix(rewritten, "\n") {
		rewritten = strings.TrimSuffix(rewritten, "\n")
	}

	return os.WriteFile(path, []byte(rewritten), 0644)
}

// isUnreleasedHeading reports whether line is a Keep-a-Changelog "Unreleased"
// H2 heading. Accepts both the bare "## [Unreleased]" form and the linked
// "## [Unreleased](https://…)" form.
func isUnreleasedHeading(line string) bool {
	if !strings.HasPrefix(line, "## ") {
		return false
	}
	return strings.Contains(line, "[Unreleased]") || strings.Contains(line, "[unreleased]")
}

// legacyVersionHeaderRE matches the "## [X.Y.Z] - YYYY-MM-DD" form devctl's
// previous (create-release-pr) flow used. Headers already rewritten to the
// inline-link form ("## [X.Y.Z](URL) (YYYY-MM-DD)") do not match, so the
// rewrite is idempotent.
var legacyVersionHeaderRE = regexp.MustCompile(`^## \[(\d+\.\d+\.\d+)\] - (\d{4}-\d{2}-\d{2})\s*$`)

// LinkChangelogVersionHeaders rewrites legacy "## [X.Y.Z] - YYYY-MM-DD" headers
// in path to the inline-link form release-please emits for new releases:
//
//	## [X.Y.Z](https://<host>/<owner>/<repo>/compare/vPREV...vX.Y.Z) (YYYY-MM-DD)
//
// PREV is the version of the next "## [V.W.Z]" heading appearing below the
// current one in the file — CHANGELOGs are always descending, so the next
// heading down is the previous release. The oldest version in the file has no
// "previous"; it falls back to a release-tag link
// ("https://<host>/<owner>/<repo>/releases/tag/vX.Y.Z").
//
// Idempotent: already-linked headers don't match legacyVersionHeaderRE. No-op
// when the file doesn't exist, contains no legacy headers, or the origin
// remote URL can't be parsed as GitHub-style (better to leave the file alone
// than to write half-formed URLs).
//
// Used after a repo opts into release-please so historical headers carry the
// same compare-URL style release-please uses for new versions — otherwise the
// file ends up half-linked (new releases yes, old releases no).
func LinkChangelogVersionHeaders(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	host, ownerRepo, ok := originHostOwnerRepo()
	if !ok {
		// No usable origin remote — don't write URLs we can't form.
		return nil
	}

	hadTrailingNewline := strings.HasSuffix(string(data), "\n")
	lines := strings.Split(string(data), "\n")

	// First pass: collect indices and versions of legacy headers in file
	// order (top-to-bottom = newest-to-oldest in a Keep-a-Changelog file).
	type entry struct {
		idx     int
		version string
		date    string
	}
	var entries []entry
	for i, line := range lines {
		if m := legacyVersionHeaderRE.FindStringSubmatch(line); m != nil {
			entries = append(entries, entry{idx: i, version: m[1], date: m[2]})
		}
	}
	if len(entries) == 0 {
		return nil
	}

	// Second pass: rewrite each. The previous version is the next entry below
	// in the file; the bottom-most entry uses the release-tag fallback.
	changed := false
	for i, e := range entries {
		var newLine string
		if i < len(entries)-1 {
			prev := entries[i+1].version
			newLine = fmt.Sprintf("## [%s](https://%s/%s/compare/v%s...v%s) (%s)", e.version, host, ownerRepo, prev, e.version, e.date)
		} else {
			newLine = fmt.Sprintf("## [%s](https://%s/%s/releases/tag/v%s) (%s)", e.version, host, ownerRepo, e.version, e.date)
		}
		if lines[e.idx] != newLine {
			lines[e.idx] = newLine
			changed = true
		}
	}

	if !changed {
		return nil
	}

	rewritten := strings.Join(lines, "\n")
	if !hadTrailingNewline && strings.HasSuffix(rewritten, "\n") {
		rewritten = strings.TrimSuffix(rewritten, "\n")
	}
	return os.WriteFile(path, []byte(rewritten), 0644)
}

// originHostOwnerRepo derives (host, "owner/repo", ok) from
// `git remote get-url origin`. Recognises the common Git URL forms:
//
//   - SSH shorthand:  git@<host>:<owner>/<repo>[.git]
//   - SSH scheme:     ssh://[user@]<host>/<owner>/<repo>[.git]
//   - HTTPS:          https://<host>/<owner>/<repo>[.git]
//
// The ssh:// form matters because a `url.<https://github.com/>.insteadOf` git
// config (common on developer machines) silently rewrites an HTTPS remote into
// `ssh://git@github.com/...` at "remote add" time, so even repos whose origin
// was added as HTTPS look like ssh:// to subsequent `git remote get-url`.
//
// Returns ok=false for anything else, so non-recognised remotes are treated as
// "leave the file alone" rather than written with a half-formed URL.
func originHostOwnerRepo() (host, ownerRepo string, ok bool) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", false
	}
	url := strings.TrimSpace(string(out))

	switch {
	case strings.HasPrefix(url, "git@"):
		// git@host:owner/repo[.git]
		rest := strings.TrimPrefix(url, "git@")
		colon := strings.Index(rest, ":")
		if colon < 0 {
			return "", "", false
		}
		return validateHostOwnerRepo(rest[:colon], rest[colon+1:])

	case strings.HasPrefix(url, "ssh://"):
		// ssh://[user@]host/owner/repo[.git]
		rest := strings.TrimPrefix(url, "ssh://")
		if at := strings.Index(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return "", "", false
		}
		return validateHostOwnerRepo(rest[:slash], rest[slash+1:])

	case strings.HasPrefix(url, "https://"):
		// https://host/owner/repo[.git]
		rest := strings.TrimPrefix(url, "https://")
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return "", "", false
		}
		return validateHostOwnerRepo(rest[:slash], rest[slash+1:])
	}

	return "", "", false
}

func validateHostOwnerRepo(h, p string) (host, ownerRepo string, ok bool) {
	p = strings.TrimSuffix(p, ".git")
	if h == "" || !strings.Contains(p, "/") {
		return "", "", false
	}
	return h, p, true
}
