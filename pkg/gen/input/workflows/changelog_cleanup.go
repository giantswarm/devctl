package workflows

import (
	"os"
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
