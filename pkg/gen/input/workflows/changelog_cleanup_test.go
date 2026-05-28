package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func Test_RemoveChangelogUnreleasedSection(t *testing.T) {
	const preamble = `# Changelog

All notable changes to this project will be documented in this file.

`
	const v018 = `## [0.1.8] - 2026-05-28

### Added

- Something.
`
	const v017 = `## [0.1.7] - 2026-05-27

### Fixed

- Bug.
`

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "unreleased with content is removed",
			input:    preamble + "## [Unreleased]\n\n### Added\n\n- New thing.\n\n" + v018 + "\n" + v017,
			expected: preamble + v018 + "\n" + v017,
		},
		{
			name:     "empty unreleased is removed",
			input:    preamble + "## [Unreleased]\n\n" + v018 + "\n" + v017,
			expected: preamble + v018 + "\n" + v017,
		},
		{
			name:     "no unreleased section is a no-op",
			input:    preamble + v018 + "\n" + v017,
			expected: preamble + v018 + "\n" + v017,
		},
		{
			name:     "linked unreleased form is recognised",
			input:    preamble + "## [Unreleased](https://example.com/compare/v0.1.8...HEAD)\n\n### Added\n\n- New.\n\n" + v018,
			expected: preamble + v018,
		},
		{
			// Degenerate: removing Unreleased leaves a trailing blank line in
			// the preamble that no longer separates anything. We trim it so
			// the file ends with a single POSIX-style terminating newline.
			name:     "unreleased as the only versioned section trims to preamble (no trailing blank)",
			input:    preamble + "## [Unreleased]\n\n### Added\n\n- Stub.\n",
			expected: "# Changelog\n\nAll notable changes to this project will be documented in this file.\n",
		},
		{
			name:     "preserves file with no trailing newline",
			input:    preamble + "## [Unreleased]\n\n- x\n\n## [0.1.0] - 2026-05-28\n\n- y",
			expected: preamble + "## [0.1.0] - 2026-05-28\n\n- y",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "CHANGELOG.md")
			if err := os.WriteFile(path, []byte(tc.input), 0644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			if err := RemoveChangelogUnreleasedSection(path); err != nil {
				t.Fatalf("RemoveChangelogUnreleasedSection: %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != tc.expected {
				t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", string(got), tc.expected)
			}
		})
	}
}

func Test_RemoveChangelogUnreleasedSection_MissingFile(t *testing.T) {
	// Calling on a non-existent path must be a clean no-op (no error), so that
	// repos without a CHANGELOG.md don't break a release-please gen run.
	if err := RemoveChangelogUnreleasedSection(filepath.Join(t.TempDir(), "does-not-exist.md")); err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
}

func Test_LinkChangelogVersionHeaders(t *testing.T) {
	preamble := "# Changelog\n\nAll notable changes...\n\n"

	testCases := []struct {
		name     string
		remote   string // origin URL set on the temp repo
		input    string
		expected string
	}{
		{
			name:   "rewrites three versions; oldest gets release-tag fallback",
			remote: "git@github.com:giantswarm/test-repo.git",
			input: preamble +
				"## [1.2.0] - 2026-04-01\n\n- mid\n\n" +
				"## [1.1.0] - 2026-03-01\n\n- prior\n\n" +
				"## [1.0.0] - 2026-01-01\n\n- first\n",
			expected: preamble +
				"## [1.2.0](https://github.com/giantswarm/test-repo/compare/v1.1.0...v1.2.0) (2026-04-01)\n\n- mid\n\n" +
				"## [1.1.0](https://github.com/giantswarm/test-repo/compare/v1.0.0...v1.1.0) (2026-03-01)\n\n- prior\n\n" +
				"## [1.0.0](https://github.com/giantswarm/test-repo/releases/tag/v1.0.0) (2026-01-01)\n\n- first\n",
		},
		{
			name:   "single legacy version gets release-tag fallback",
			remote: "https://github.com/giantswarm/test-repo.git",
			input:  preamble + "## [0.1.0] - 2026-05-01\n\n- first\n",
			expected: preamble +
				"## [0.1.0](https://github.com/giantswarm/test-repo/releases/tag/v0.1.0) (2026-05-01)\n\n- first\n",
		},
		{
			name:   "already-linked headers are untouched (idempotent)",
			remote: "git@github.com:giantswarm/test-repo.git",
			input: preamble +
				"## [1.0.0](https://github.com/giantswarm/test-repo/releases/tag/v1.0.0) (2026-01-01)\n\n- first\n",
			expected: preamble +
				"## [1.0.0](https://github.com/giantswarm/test-repo/releases/tag/v1.0.0) (2026-01-01)\n\n- first\n",
		},
		{
			name:   "mixed: only legacy headers rewritten, already-linked ones left alone",
			remote: "git@github.com:giantswarm/test-repo.git",
			input: preamble +
				"## [2.0.0](https://github.com/giantswarm/test-repo/compare/v1.0.0...v2.0.0) (2026-05-28)\n\n- shiny\n\n" +
				"## [1.0.0] - 2026-01-01\n\n- old style\n",
			expected: preamble +
				"## [2.0.0](https://github.com/giantswarm/test-repo/compare/v1.0.0...v2.0.0) (2026-05-28)\n\n- shiny\n\n" +
				"## [1.0.0](https://github.com/giantswarm/test-repo/releases/tag/v1.0.0) (2026-01-01)\n\n- old style\n",
		},
		{
			name:   "host other than github.com (GHE) is honoured",
			remote: "git@github.enterprise.example.com:org/repo.git",
			input:  preamble + "## [1.0.0] - 2026-01-01\n\n- first\n",
			expected: preamble +
				"## [1.0.0](https://github.enterprise.example.com/org/repo/releases/tag/v1.0.0) (2026-01-01)\n\n- first\n",
		},
		{
			// A user-level `url.<…>.insteadOf` config can rewrite HTTPS remotes
			// to ssh://git@host/owner/repo at "remote add" time, so origins
			// surface as ssh:// to subsequent `git remote get-url`. Must be
			// parsed identically.
			name:   "ssh:// scheme form (insteadOf-rewritten HTTPS)",
			remote: "ssh://git@github.com/giantswarm/test-repo.git",
			input:  preamble + "## [1.0.0] - 2026-01-01\n\n- first\n",
			expected: preamble +
				"## [1.0.0](https://github.com/giantswarm/test-repo/releases/tag/v1.0.0) (2026-01-01)\n\n- first\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Set up a minimal git repo with the origin remote.
			runGit(t, dir, "init", "-q")
			runGit(t, dir, "remote", "add", "origin", tc.remote)

			path := filepath.Join(dir, "CHANGELOG.md")
			if err := os.WriteFile(path, []byte(tc.input), 0644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			// Run from inside the repo dir so `git remote get-url origin` resolves.
			withCwd(t, dir, func() {
				if err := LinkChangelogVersionHeaders(path); err != nil {
					t.Fatalf("LinkChangelogVersionHeaders: %v", err)
				}
			})

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != tc.expected {
				t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", string(got), tc.expected)
			}

			// Re-run: must be a clean no-op (idempotent).
			withCwd(t, dir, func() {
				if err := LinkChangelogVersionHeaders(path); err != nil {
					t.Fatalf("second run: %v", err)
				}
			})
			got2, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back (second): %v", err)
			}
			if string(got2) != tc.expected {
				t.Errorf("not idempotent\n--- after second run ---\n%s\n--- want ---\n%s", string(got2), tc.expected)
			}
		})
	}
}

func Test_LinkChangelogVersionHeaders_NoLegacyHeaders(t *testing.T) {
	// File contains only already-linked headers and a preamble — no rewrite,
	// no error.
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:giantswarm/test-repo.git")
	path := filepath.Join(dir, "CHANGELOG.md")
	body := "# Changelog\n\n## [0.1.0](https://github.com/giantswarm/test-repo/releases/tag/v0.1.0) (2026-05-01)\n\n- one\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	withCwd(t, dir, func() {
		if err := LinkChangelogVersionHeaders(path); err != nil {
			t.Fatalf("err: %v", err)
		}
	})
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Errorf("file mutated despite no legacy headers\n--- got ---\n%s\n--- want ---\n%s", string(got), body)
	}
}

func Test_LinkChangelogVersionHeaders_MissingFile(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:giantswarm/test-repo.git")
	withCwd(t, dir, func() {
		if err := LinkChangelogVersionHeaders(filepath.Join(dir, "does-not-exist.md")); err != nil {
			t.Fatalf("expected no error for missing file, got: %v", err)
		}
	})
}

func Test_LinkChangelogVersionHeaders_NoUsableRemote(t *testing.T) {
	// Origin remote is not GitHub-style — the function must leave the file
	// alone (better than writing half-formed URLs).
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "remote", "add", "origin", "file:///tmp/some-local-repo.git")
	path := filepath.Join(dir, "CHANGELOG.md")
	body := "# Changelog\n\n## [1.0.0] - 2026-01-01\n\n- first\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	withCwd(t, dir, func() {
		if err := LinkChangelogVersionHeaders(path); err != nil {
			t.Fatalf("err: %v", err)
		}
	})
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Errorf("file mutated despite non-GitHub remote\n--- got ---\n%s\n--- want ---\n%s", string(got), body)
	}
}

// runGit is a tiny helper for setting up git state in temp-dir tests.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed in %s: %v\n%s", args, dir, err, string(out))
	}
}

// withCwd runs fn after chdir'ing to dir, restoring the original cwd on return.
// Necessary because LinkChangelogVersionHeaders shells out to `git remote get-url
// origin`, which resolves relative to cwd. Tests can't share state via cwd
// otherwise (Go tests in the same package run sequentially by default unless
// t.Parallel() is called — which none of these do).
func withCwd(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	fn()
}
