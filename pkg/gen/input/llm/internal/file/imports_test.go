package file

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

var testInstructionFiles = []string{"AGENTS.md", "CLAUDE.md", ".claude/CLAUDE.md"}

func Test_MoveInstructions(t *testing.T) {
	testCases := []struct {
		name     string
		content  string
		dir      string
		existing []string
		want     string
	}{
		{
			name:    "an import of AGENTS.md is dropped with the blank lines after it",
			content: "@AGENTS.md\n\nRun make test.\n",
			dir:     ".",
			want:    "Run make test.\n",
		},
		{
			name:    "after a byte order mark",
			content: "\ufeff@AGENTS.md\n\nRun make test.\n",
			dir:     ".",
			want:    "\ufeffRun make test.\n",
		},
		{
			name:    "in a code fence it is an example",
			content: "Example:\n```\n@AGENTS.md\n```\n",
			dir:     ".",
			want:    "Example:\n```\n@AGENTS.md\n```\n",
		},
		{
			name:    "an inline import becomes a mention",
			content: "Read @AGENTS.md first, then @./AGENTS.md.\n",
			dir:     ".",
			want:    "Read AGENTS.md first, then ./AGENTS.md.\n",
		},
		{
			name:     "imports of instruction files from .claude",
			content:  "@../CLAUDE.md\n@CLAUDE.md\n\nSee @notes.md.\n",
			dir:      ".claude",
			existing: []string{"CLAUDE.md", ".claude/CLAUDE.md", ".claude/notes.md"},
			want:     "See @.claude/notes.md.\n",
		},
		{
			name:     "relative links are rebased when their target exists",
			content:  "See [notes](notes.md#setup), [docs](../docs/a.md \"Docs\"), [web](https://example.com/a.md), [top](#top) and [missing](missing.md).\n\n[ref]: notes.md\n",
			dir:      ".claude",
			existing: []string{".claude/notes.md", "docs/a.md"},
			want:     "See [notes](.claude/notes.md#setup), [docs](docs/a.md \"Docs\"), [web](https://example.com/a.md), [top](#top) and [missing](missing.md).\n\n[ref]: .claude/notes.md\n",
		},
		{
			name:     "an @ right after inline code is not an import",
			content:  "Run `make`@notes.md and `@notes.md`.\n",
			dir:      ".claude",
			existing: []string{".claude/notes.md"},
			want:     "Run `make`@notes.md and `@notes.md`.\n",
		},
		{
			name:     "mentions and addresses stay",
			content:  "Ask @giantswarm/team-x or team@example.com.\n",
			dir:      ".claude",
			existing: []string{".claude/notes.md"},
			want:     "Ask @giantswarm/team-x or team@example.com.\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exists := func(rel string) bool { return slices.Contains(tc.existing, rel) }
			got := MoveInstructions([]byte(tc.content), tc.dir, exists, testInstructionFiles)
			require.Equal(t, tc.want, string(got))
		})
	}
}

func Test_AddImport(t *testing.T) {
	testCases := []struct {
		name    string
		content string
		want    string
	}{
		{name: "added on top", content: "Local.\n", want: "@AGENTS.md\n\nLocal.\n"},
		{name: "an import with trailing punctuation counts", content: "Read @AGENTS.md.\n", want: "Read @AGENTS.md.\n"},
		{name: "an example in a fence does not count", content: "```\n@AGENTS.md\n```\n", want: "@AGENTS.md\n\n```\n@AGENTS.md\n```\n"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, string(AddImport([]byte(tc.content), "AGENTS.md")))
		})
	}
}
