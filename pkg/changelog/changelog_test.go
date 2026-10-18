package changelog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
)

const preamble = `# Changelog

All notable changes to this project will be documented in this file.
`

func release(t *testing.T, version, date, changelog string) Release {
	t.Helper()
	return Release{Version: semver.MustParse(version), Tag: "v" + version, Date: date, Changelog: changelog}
}

func TestCut(t *testing.T) {
	// 1.1.0 released the feature, 1.2.0 the fix; the docs entry is new.
	at110 := preamble + `
## [Unreleased]

### Added

- A feature.

## [1.0.0] - 2026-01-01

### Added

- The first release.

[Unreleased]: https://github.com/giantswarm/example/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/giantswarm/example/releases/tag/v1.0.0
`
	at120 := strings.Replace(at110, "### Added\n\n- A feature.\n", "### Added\n\n- A feature.\n\n### Fixed\n\n- A fix,\n  over two lines.\n", 1)
	head := strings.Replace(at120, "- A feature.\n", "- A feature.\n\n### Changed\n\n- Docs, not released yet.\n", 1)

	releases := []Release{
		release(t, "1.2.0", "2026-03-01", at120),
		release(t, "1.0.0", "2026-01-01", ""),
		release(t, "1.1.0", "2026-02-01", at110),
	}

	got, moved := Cut(head, releases)

	want := preamble + `
## [Unreleased]

### Changed

- Docs, not released yet.

## [1.2.0] - 2026-03-01

### Fixed

- A fix,
  over two lines.

## [1.1.0] - 2026-02-01

### Added

- A feature.

## [1.0.0] - 2026-01-01

### Added

- The first release.

[Unreleased]: https://github.com/giantswarm/example/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/giantswarm/example/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/giantswarm/example/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/giantswarm/example/releases/tag/v1.0.0
`
	if got != want {
		t.Fatalf("Cut:\n%s\nwant:\n%s", got, want)
	}
	if len(moved) != 2 || moved[0].Release != "v1.1.0" || moved[1].Release != "v1.2.0" {
		t.Fatalf("moved = %+v, want the feature in v1.1.0 and the fix in v1.2.0", moved)
	}

	// A second cut has nothing left to move.
	again, moved := Cut(got, releases)
	if again != got || len(moved) != 0 {
		t.Fatalf("second Cut moved %+v", moved)
	}
}

func TestCutKeepsUntouchedSections(t *testing.T) {
	// The released section keeps its blank lines between entries and its
	// fenced block, byte for byte.
	old := `## [1.0.0] - 2026-01-01

### Added

- One.

- Two, with

  ` + "```" + `
  - not an entry
  ` + "```" + `
`
	head := preamble + "\n## [Unreleased]\n\n### Added\n\n- New.\n\n" + old
	at := preamble + "\n## [Unreleased]\n\n### Added\n\n- New.\n\n" + old

	got, moved := Cut(head, []Release{release(t, "1.1.0", "2026-02-01", at)})
	if len(moved) != 1 {
		t.Fatalf("moved = %+v, want one", moved)
	}
	if !strings.HasSuffix(got, old) {
		t.Fatalf("the 1.0.0 section changed:\n%s", got)
	}
	if !strings.Contains(got, "## [Unreleased]\n\n## [1.1.0] - 2026-02-01\n\n### Added\n\n- New.\n\n## [1.0.0]") {
		t.Fatalf("Cut:\n%s", got)
	}
}

func TestCutCreditsACopyAddedAgain(t *testing.T) {
	// "- Bump dependencies." was released in 1.1.0, never cut, and added again
	// for 1.2.0: each release is credited with one copy.
	at110 := preamble + "\n## [Unreleased]\n\n### Changed\n\n- Bump dependencies.\n"
	at120 := preamble + "\n## [Unreleased]\n\n### Changed\n\n- Bump dependencies.\n- Bump dependencies.\n"
	head := at120 + "- Bump dependencies.\n"

	got, moved := Cut(head, []Release{
		release(t, "1.1.0", "2026-02-01", at110),
		release(t, "1.2.0", "2026-03-01", at120),
	})
	if len(moved) != 2 || moved[0].Release != "v1.1.0" || moved[1].Release != "v1.2.0" {
		t.Fatalf("moved = %+v", moved)
	}
	if !strings.Contains(got, "## [Unreleased]\n\n### Changed\n\n- Bump dependencies.\n\n## [1.2.0]") {
		t.Fatalf("the third copy is not released yet:\n%s", got)
	}
}

func TestCutWithoutUnreleased(t *testing.T) {
	text := preamble + "\n## [1.0.0]\n\n- One.\n"
	got, moved := Cut(text, []Release{release(t, "1.0.0", "2026-01-01", text)})
	if got != text || moved != nil {
		t.Fatalf("Cut changed a file without Unreleased: %q %+v", got, moved)
	}
	if HasUnreleased(text) {
		t.Fatal("HasUnreleased = true")
	}
}

// repo is a git repository in a temporary directory, run runs git in it and
// write writes its CHANGELOG.md.
func repo(t *testing.T) (dir string, run func(args ...string) string, write func(text string)) {
	t.Helper()
	dir = t.TempDir()
	run = func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...) // #nosec G204 -- fixed binary, args are built by this test, test-only
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_AUTHOR_DATE=2026-02-01T12:00:00Z", "GIT_COMMITTER_DATE=2026-02-01T12:00:00Z",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	write = func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, run, write
}

func TestReleases(t *testing.T) {
	dir, run, write := repo(t)

	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "chore: start")
	run("tag", "v0.1.0") // before the file
	write("one\n")
	run("add", ".")
	run("commit", "-q", "-m", "feat: one")
	run("tag", "v1.0.0-rc.1")
	run("tag", "-a", "-m", "promoted", "v1.0.0") // the promoted candidate, annotated
	run("tag", "not-a-release")
	write("two\n")
	run("commit", "-q", "-am", "fix: two")
	run("tag", "v1.0.1-rc.1")
	run("checkout", "-q", "-b", "other", "HEAD~1")
	run("commit", "-q", "--allow-empty", "-m", "fix: elsewhere")
	run("tag", "v9.0.0") // not reachable from main
	run("checkout", "-q", "main")

	got, err := Releases(dir, "CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got {
		names = append(names, r.Tag+"="+strings.TrimSpace(r.Changelog)+"@"+r.Date)
	}
	want := "v0.1.0=@2026-02-01 v1.0.0=one@2026-02-01 v1.0.1-rc.1=two@2026-02-01"
	if strings.Join(names, " ") != want {
		t.Fatalf("Releases = %v, want %s", names, want)
	}
}

func TestCutReclaimsAnEntryABranchUpdateMisplaced(t *testing.T) {
	// v1.1.0 released "- Released.": its cut merges while a pull request with
	// its own entry is open, and the pull request's branch update merges the
	// cut. git puts the pull request's entry under the cut's new heading.
	at110 := preamble + `
## [Unreleased]

### Fixed

- Released.

## [1.0.0] - 2026-01-01

### Added

- The first release.

[Unreleased]: https://github.com/giantswarm/example/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/giantswarm/example/releases/tag/v1.0.0
`
	v110 := release(t, "1.1.0", "2026-02-01", at110)
	cut, moved := Cut(at110, []Release{v110})
	if len(moved) != 1 {
		t.Fatalf("the v1.1.0 cut moved %+v", moved)
	}

	for name, pr := range map[string]string{
		"at the group's top":    strings.Replace(at110, "- Released.\n", "- The pull request's fix.\n- Released.\n", 1),
		"at the group's bottom": strings.Replace(at110, "- Released.\n", "- Released.\n- The pull request's fix.\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, run, write := repo(t)
			run("init", "-q", "-b", "main")
			write(at110)
			run("add", ".")
			run("commit", "-q", "-m", "fix: released")
			run("checkout", "-q", "-b", "pr")
			write(pr)
			run("commit", "-q", "-am", "fix: the pull request's fix")
			run("checkout", "-q", "main")
			write(cut)
			run("commit", "-q", "-am", "docs(changelog): cut released entries out of Unreleased")
			run("checkout", "-q", "pr")
			run("merge", "-q", "--no-edit", "main") // the branch update: clean
			merged := run("show", "HEAD:CHANGELOG.md")
			if !strings.Contains(merged, "## [Unreleased]\n\n## [1.1.0] - 2026-02-01\n\n### Fixed\n\n") ||
				strings.Index(merged, "- The pull request's fix.") < strings.Index(merged, "## [1.1.0]") {
				t.Fatalf("the merge no longer misplaces the entry, the case this test reproduces:\n%s", merged)
			}

			// The pull request's check on its merge: its entry is no release's.
			if own, pending := Pending(merged, cut, []Release{v110}); len(own) != 0 || len(pending) != 0 {
				t.Fatalf("check: own = %+v pending = %+v, want none", own, pending)
			}

			// The pull request merges and v1.2.0 carries the entry under
			// [1.1.0]: the v1.2.0 cut moves it into v1.2.0's section.
			releases := []Release{v110, release(t, "1.2.0", "2026-03-01", merged)}
			got, moved := Cut(merged, releases)
			want := preamble + `
## [Unreleased]

## [1.2.0] - 2026-03-01

### Fixed

- The pull request's fix.

## [1.1.0] - 2026-02-01

### Fixed

- Released.

## [1.0.0] - 2026-01-01

### Added

- The first release.

[Unreleased]: https://github.com/giantswarm/example/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/giantswarm/example/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/giantswarm/example/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/giantswarm/example/releases/tag/v1.0.0
`
			if got != want {
				t.Fatalf("Cut:\n%s\nwant:\n%s", got, want)
			}
			if len(moved) != 1 || moved[0] != (Moved{Release: "v1.2.0", Entry: "- The pull request's fix."}) {
				t.Fatalf("moved = %+v, want the pull request's fix in v1.2.0", moved)
			}
			if again, moved := Cut(got, releases); again != got || len(moved) != 0 {
				t.Fatalf("second Cut moved %+v", moved)
			}
		})
	}
}

func TestCutKeepsSectionsItDidNotCreate(t *testing.T) {
	// 1.1.0's tag holds its own section: an entry added to it later is a
	// correction, not a stray. 1.2.0's tag holds nothing of the section a
	// person wrote for it later.
	old := "## [1.2.0] - 2026-03-01\n\n- Written later.\n\n## [1.1.0] - 2026-02-01\n\n- One.\n- Added later.\n"
	at110 := preamble + "\n## [Unreleased]\n\n## [1.1.0] - 2026-02-01\n\n- One.\n"
	at120 := preamble + "\n## [Unreleased]\n\n- Two.\n\n## [1.1.0] - 2026-02-01\n\n- One.\n"
	head := preamble + "\n## [Unreleased]\n\n- New.\n\n" + old

	got, moved := Cut(head, []Release{
		release(t, "1.1.0", "2026-02-01", at110),
		release(t, "1.2.0", "2026-03-01", at120),
	})
	if got != head || len(moved) != 0 {
		t.Fatalf("Cut moved %+v:\n%s", moved, got)
	}
}

func TestCutLinksSectionsCreatedTogether(t *testing.T) {
	// The newer release's entry comes first, so its section is created
	// first: its link still compares with the older release created after it.
	at110 := preamble + "\n## [Unreleased]\n\n- Old.\n\n## [1.0.0]\n\n- First.\n\n" +
		"[Unreleased]: https://github.com/giantswarm/example/compare/v1.0.0...HEAD\n" +
		"[1.0.0]: https://github.com/giantswarm/example/releases/tag/v1.0.0\n"
	at120 := strings.Replace(at110, "- Old.\n", "- New.\n- Old.\n", 1)

	got, _ := Cut(at120, []Release{
		release(t, "1.1.0", "2026-02-01", at110),
		release(t, "1.2.0", "2026-03-01", at120),
	})
	want := "[Unreleased]: https://github.com/giantswarm/example/compare/v1.2.0...HEAD\n" +
		"[1.2.0]: https://github.com/giantswarm/example/compare/v1.1.0...v1.2.0\n" +
		"[1.1.0]: https://github.com/giantswarm/example/compare/v1.0.0...v1.1.0\n" +
		"[1.0.0]: https://github.com/giantswarm/example/releases/tag/v1.0.0\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("Cut:\n%s\nwant links:\n%s", got, want)
	}
}

func TestPending(t *testing.T) {
	at110 := preamble + "\n## [Unreleased]\n\n- Old.\n- Twice.\n"
	at120 := preamble + "\n## [Unreleased]\n\n- Old.\n- Twice.\n- Twice.\n- Newer.\n"
	releases := []Release{release(t, "1.1.0", "2026-02-01", at110), release(t, "1.2.0", "2026-03-01", at120)}

	// The base holds Old., one Twice. and Newer. released: the pull request
	// adds New. and the second copy of Twice. comes back on its own.
	base := preamble + "\n## [Unreleased]\n\n- Old.\n- Twice.\n- Newer.\n"
	head := preamble + "\n## [Unreleased]\n\n- New.\n- Old.\n- Twice.\n- Twice.\n- Newer.\n"
	own, pending := Pending(head, base, releases)
	if len(own) != 1 || own[0] != (Moved{Release: "v1.2.0", Entry: "- Twice."}) {
		t.Fatalf("own = %+v, want the second Twice. of v1.2.0", own)
	}
	if len(pending) != 3 {
		t.Fatalf("pending = %+v, want Old., Twice. and Newer.", pending)
	}

	// Without a base every released entry is the pull request's own.
	if own, pending := Pending(head, "", releases); len(own) != 4 || len(pending) != 0 {
		t.Fatalf("without a base: own = %+v pending = %+v", own, pending)
	}
}
