package llm_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm"
)

const (
	testBody  = "- Use conventional commits.\n"
	testBlock = "<!-- BEGIN devctl-managed: edits in this block are overwritten -->\n<!-- markdownlint-disable-file MD041 -->\n\n" + testBody + "\n<!-- END devctl-managed -->\n"
)

func TestMigration_Instructions(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		links map[string]string
		want  map[string]string
	}{
		{
			name: "no instructions",
			want: map[string]string{"AGENTS.md": testBlock + "\n## This repository\n"},
		},
		{
			name:  "root CLAUDE.md moves and becomes an import",
			files: map[string]string{"CLAUDE.md": "# CLAUDE.md\n\nRun make test.\n"},
			want: map[string]string{
				"AGENTS.md": testBlock + "\n# CLAUDE.md\n\nRun make test.\n",
				"CLAUDE.md": "@AGENTS.md\n",
			},
		},
		{
			name:  ".claude/CLAUDE.md moves and is deleted",
			files: map[string]string{".claude/CLAUDE.md": "Run make test.\n", ".claude/settings.json": "{}\n"},
			want: map[string]string{
				"AGENTS.md":             testBlock + "\nRun make test.\n",
				".claude/settings.json": "{}\n",
			},
		},
		{
			name:  "root CLAUDE.md moves, .claude/CLAUDE.md stays and imports AGENTS.md",
			files: map[string]string{"CLAUDE.md": "Shared.\n", ".claude/CLAUDE.md": "Claude only.\n"},
			want: map[string]string{
				"AGENTS.md":         testBlock + "\nShared.\n",
				"CLAUDE.md":         "@AGENTS.md\n",
				".claude/CLAUDE.md": "@../AGENTS.md\n\nClaude only.\n",
			},
		},
		{
			name:  "AGENTS.md keeps its content",
			files: map[string]string{"AGENTS.md": "Run make test.\n"},
			want:  map[string]string{"AGENTS.md": testBlock + "\nRun make test.\n"},
		},
		{
			name:  "Claude files with their own content stay and import AGENTS.md",
			files: map[string]string{"AGENTS.md": "Shared.\n", "CLAUDE.md": "# CLAUDE.md\n\nSee AGENTS.md.\n", ".claude/CLAUDE.md": "Claude only.\n"},
			want: map[string]string{
				"AGENTS.md":         testBlock + "\nShared.\n",
				"CLAUDE.md":         "@AGENTS.md\n\n# CLAUDE.md\n\nSee AGENTS.md.\n",
				".claude/CLAUDE.md": "@../AGENTS.md\n\nClaude only.\n",
			},
		},
		{
			name:  "existing imports and links stay",
			files: map[string]string{"AGENTS.md": "Shared.\n", "CLAUDE.md": "Intro.\n@./AGENTS.md\n"},
			links: map[string]string{".claude/CLAUDE.md": "../AGENTS.md"},
			want: map[string]string{
				"AGENTS.md":         testBlock + "\nShared.\n",
				"CLAUDE.md":         "Intro.\n@./AGENTS.md\n",
				".claude/CLAUDE.md": "-> ../AGENTS.md",
			},
		},
		{
			name:  "a moved file does not import itself",
			files: map[string]string{"CLAUDE.md": "@AGENTS.md\n\nRun make test.\n"},
			want: map[string]string{
				"AGENTS.md": testBlock + "\nRun make test.\n",
				"CLAUDE.md": "@AGENTS.md\n",
			},
		},
		{
			name:  "a moved .claude/CLAUDE.md does not import AGENTS.md through a link",
			files: map[string]string{".claude/CLAUDE.md": "@../CLAUDE.md\n\nClaude only.\n"},
			links: map[string]string{"CLAUDE.md": "AGENTS.md"},
			want: map[string]string{
				"AGENTS.md": testBlock + "\nClaude only.\n",
				"CLAUDE.md": "-> AGENTS.md",
			},
		},
		{
			name:  "a link to the missing AGENTS.md is kept",
			links: map[string]string{"CLAUDE.md": "AGENTS.md"},
			want: map[string]string{
				"AGENTS.md": testBlock + "\n## This repository\n",
				"CLAUDE.md": "-> AGENTS.md",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := newCheckout(t)
			writeFiles(t, root, tc.files)
			writeLinks(t, root, tc.links)

			migrate(t, root)
			require.Equal(t, tc.want, readTree(t, root))

			migrate(t, root)
			require.Equal(t, tc.want, readTree(t, root), "a repeated migration must not change the repository")
		})
	}
}

func migrate(t *testing.T, root string) {
	t.Helper()
	apply(t, root, llm.MigrationMode)
}

func apply(t *testing.T, root string, mode llm.Mode) {
	t.Helper()
	changes, report, err := llm.Plan(t.Context(), root, mode, testBody)
	require.NoError(t, err)
	require.Empty(t, report.Findings)
	require.NoError(t, changes.Apply(t.Context()))
}

// readTree returns the files in root by slash path, and a link as "-> target".
// Directories that only exist to hold files are implied by them.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	r, err := os.OpenRoot(root)
	require.NoError(t, err)
	defer r.Close() //nolint:errcheck

	tree := map[string]string{}
	err = fs.WalkDir(r.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case rel == ".git":
			return fs.SkipDir
		case d.Type()&fs.ModeSymlink != 0:
			target, err := r.Readlink(rel)
			if err != nil {
				return err
			}
			tree[rel] = "-> " + target
		case d.Type().IsRegular():
			content, err := r.ReadFile(rel)
			if err != nil {
				return err
			}
			tree[rel] = string(content)
		}
		return nil
	})
	require.NoError(t, err)
	return tree
}

func TestMigration_RebasesMovedImports(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{
		"docs/conventions.md": "Conventions.\n",
		".claude/notes.md":    "Notes.\n",
		".claude/CLAUDE.md": "See @../docs/conventions.md and @notes.md here.\n" +
			"Ask @giantswarm/team-x, or mail team@example.com.\n" +
			"`@../docs/conventions.md` is an example.\n" +
			"```\n@../docs/conventions.md\n```\n" +
			"@~/.claude/mine.md\n",
	})

	migrate(t, root)

	require.Equal(t, map[string]string{
		"docs/conventions.md": "Conventions.\n",
		".claude/notes.md":    "Notes.\n",
		"AGENTS.md": testBlock + "\n" +
			"See @docs/conventions.md and @.claude/notes.md here.\n" +
			"Ask @giantswarm/team-x, or mail team@example.com.\n" +
			"`@../docs/conventions.md` is an example.\n" +
			"```\n@../docs/conventions.md\n```\n" +
			"@~/.claude/mine.md\n",
	}, readTree(t, root))
}

func TestMigration_MovesSkills(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{
		".claude/skills/a/SKILL.md":       "a\n",
		".claude/skills/a/scripts/run.py": "print()\n",
		".agents/skills/b/SKILL.md":       "b\n",
		".agents/skills/UPSTREAM.md":      "Upstream.\n",
	})
	writeLinks(t, root, map[string]string{".claude/skills/b": "../../.agents/skills/b"})
	want := map[string]string{
		"AGENTS.md":                       testBlock + "\n## This repository\n",
		".agents/skills/a/SKILL.md":       "a\n",
		".agents/skills/a/scripts/run.py": "print()\n",
		".agents/skills/b/SKILL.md":       "b\n",
		".agents/skills/UPSTREAM.md":      "Upstream.\n",
		".claude/skills":                  "-> ../.agents/skills",
	}

	migrate(t, root)
	require.Equal(t, want, readTree(t, root))

	migrate(t, root)
	require.Equal(t, want, readTree(t, root))
}

func TestMigration_Steps(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{
		"CLAUDE.md":                 "Shared.\n",
		".claude/CLAUDE.md":         "Claude only.\n",
		".claude/skills/a/SKILL.md": "a\n",
		".agents/skills/b/SKILL.md": "b\n",
	})
	writeLinks(t, root, map[string]string{".claude/skills/b": "../../.agents/skills/b"})

	migration, report, err := llm.Plan(t.Context(), root, llm.MigrationMode, testBody)
	require.NoError(t, err)
	require.Empty(t, report.Findings)
	require.Equal(t, []string{
		"move the content of CLAUDE.md to AGENTS.md, below the devctl-managed block",
		"replace CLAUDE.md with an import of AGENTS.md",
		"add an import of AGENTS.md to .claude/CLAUDE.md",
		"move .claude/skills/a to .agents/skills/a",
		"remove .claude/skills/b, a link to .agents/skills/b",
		"replace .claude/skills with a link to ../.agents/skills",
	}, migration.Steps())
}

func TestMigration_BlockedRepositoryIsUnchanged(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{"CONTRIBUTING.md": "Contributing.\n"})
	writeLinks(t, root, map[string]string{"AGENTS.md": "CONTRIBUTING.md", "CLAUDE.md": "AGENTS.md"})
	before := readTree(t, root)

	migration, report, err := llm.Plan(t.Context(), root, llm.MigrationMode, testBody)
	require.NoError(t, err)
	require.Equal(t, []string{"AGENTS.md"}, findingPaths(t, report))
	require.Empty(t, migration.Steps())
	require.Equal(t, before, readTree(t, root))
}

func TestMigration_InvalidBlockChangesNothing(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{"CLAUDE.md": "Shared.\n", ".claude/skills/a/SKILL.md": "a\n"})
	before := readTree(t, root)

	_, _, err := llm.Plan(t.Context(), root, llm.MigrationMode, "<!-- END devctl-managed -->\n")
	require.Error(t, err)
	require.Equal(t, before, readTree(t, root))
}

func TestMigration_BrokenMarkersInSource(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{"CLAUDE.md": "Intro.\n<!-- END devctl-managed -->\n"})

	migration, report, err := llm.Plan(t.Context(), root, llm.MigrationMode, testBody)
	require.NoError(t, err)
	require.Equal(t, []string{"CLAUDE.md"}, findingPaths(t, report))
	require.Equal(t, "the end marker on line 2 has no begin marker", report.Findings[0].Problem)
	require.Empty(t, migration.Steps())
}

func TestMigration_SourceChangedSincePlan(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{"CLAUDE.md": "Shared.\n"})

	migration, _, err := llm.Plan(t.Context(), root, llm.MigrationMode, testBody)
	require.NoError(t, err)
	writeFiles(t, root, map[string]string{"CLAUDE.md": "Shared, edited.\n"})
	before := readTree(t, root)

	require.ErrorContains(t, migration.Apply(t.Context()), "CLAUDE.md changed")
	require.Equal(t, before, readTree(t, root))
}

const (
	legacyBase = ".cursor/rules/zz_generated.base-llm-rules.mdc"
	legacyGo   = ".cursor/rules/zz_generated.go-llm-rules.mdc"
)

// Both modes replace the legacy rules and keep the .claude/skills link.
func TestChanges_BothModes(t *testing.T) {
	testCases := []struct {
		name  string
		files map[string]string
		links map[string]string
		want  map[string]string
	}{
		{
			name:  "legacy rules are deleted, other Cursor rules stay",
			files: map[string]string{legacyBase: "old\n", legacyGo: "old\n", ".cursor/rules/custom.mdc": "Mine.\n"},
			want: map[string]string{
				"AGENTS.md":                testBlock + "\n## This repository\n",
				".cursor/rules/custom.mdc": "Mine.\n",
			},
		},
		{
			name:  "the block is updated and a Claude file without the import gets it",
			files: map[string]string{"AGENTS.md": "<!-- BEGIN devctl-managed -->\n- old\n<!-- END devctl-managed -->\n\n## This repository\n\nLocal.\n", "CLAUDE.md": "Claude only.\n"},
			want: map[string]string{
				"AGENTS.md": testBlock + "\n## This repository\n\nLocal.\n",
				"CLAUDE.md": "@AGENTS.md\n\nClaude only.\n",
			},
		},
		{
			name:  "shared skills get the Claude link",
			files: map[string]string{"AGENTS.md": testBlock, ".agents/skills/a/SKILL.md": "a\n"},
			want: map[string]string{
				"AGENTS.md":                 testBlock,
				".agents/skills/a/SKILL.md": "a\n",
				".claude/skills":            "-> ../.agents/skills",
			},
		},
		{
			name:  "an existing link stays",
			files: map[string]string{"AGENTS.md": testBlock, ".agents/skills/a/SKILL.md": "a\n"},
			links: map[string]string{".claude/skills": "../.agents/skills/"},
			want: map[string]string{
				"AGENTS.md":                 testBlock,
				".agents/skills/a/SKILL.md": "a\n",
				".claude/skills":            "-> ../.agents/skills/",
			},
		},
	}

	for _, tc := range testCases {
		for _, mode := range []llm.Mode{llm.DefaultMode, llm.MigrationMode} {
			t.Run(tc.name, func(t *testing.T) {
				root := newCheckout(t)
				writeFiles(t, root, tc.files)
				writeLinks(t, root, tc.links)

				apply(t, root, mode)
				require.Equal(t, tc.want, readTree(t, root))

				apply(t, root, mode)
				require.Equal(t, tc.want, readTree(t, root), "a repeated run must not change the repository")
			})
		}
	}
}

func TestChanges_EmptyLegacyDirectoriesAreRemoved(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{legacyBase: "old\n"})

	apply(t, root, llm.DefaultMode)

	require.NoDirExists(t, filepath.Join(root, ".cursor"))
}

func TestChanges_DefaultModeLeavesUnmigratedRepositories(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{"CLAUDE.md": "Shared.\n", legacyBase: "old\n"})
	before := readTree(t, root)

	changes, report, err := llm.Plan(t.Context(), root, llm.DefaultMode, testBody)
	require.NoError(t, err)
	require.Equal(t, []string{"CLAUDE.md"}, findingPaths(t, report))
	require.Empty(t, changes.Steps())
	require.Equal(t, before, readTree(t, root))
}

func TestChanges_StepsForLegacyRulesAndLink(t *testing.T) {
	root := newCheckout(t)
	writeFiles(t, root, map[string]string{legacyBase: "old\n", ".agents/skills/a/SKILL.md": "a\n"})

	changes, _, err := llm.Plan(t.Context(), root, llm.DefaultMode, testBody)
	require.NoError(t, err)
	require.Equal(t, []string{
		"create AGENTS.md with the devctl-managed block",
		"create .claude/skills as a link to ../.agents/skills",
		"delete " + legacyBase,
	}, changes.Steps())
}

// The cleanup must not reach outside the repository through a link.
func TestChanges_LinkedLegacyDirectoriesAreLeftAlone(t *testing.T) {
	for link, target := range map[string]string{".cursor": "shared", ".cursor/rules": "../shared/rules"} {
		t.Run(link, func(t *testing.T) {
			root := newCheckout(t)
			writeFiles(t, root, map[string]string{
				"shared/rules/zz_generated.base-llm-rules.mdc": "Shared.\n",
				"shared/zz_generated.base-llm-rules.mdc":       "Shared.\n",
			})
			writeLinks(t, root, map[string]string{link: target})

			apply(t, root, llm.DefaultMode)

			tree := readTree(t, root)
			require.Equal(t, "Shared.\n", tree["shared/rules/zz_generated.base-llm-rules.mdc"])
			require.Equal(t, "-> "+target, tree[link])
		})
	}
}

func TestChanges_UpToDateRepositoryHasNoSteps(t *testing.T) {
	for _, mode := range []llm.Mode{llm.DefaultMode, llm.MigrationMode} {
		root := newCheckout(t)
		writeFiles(t, root, map[string]string{"CLAUDE.md": "Shared.\n", ".claude/skills/a/SKILL.md": "a\n"})
		migrate(t, root)

		changes, report, err := llm.Plan(t.Context(), root, mode, testBody)
		require.NoError(t, err)
		require.Empty(t, report.Findings)
		require.Empty(t, changes.Steps(), "mode %d", mode)
	}
}

func TestChanges_EmptyClaudeFilesHoldNoInstructions(t *testing.T) {
	testCases := []struct {
		name  string
		mode  llm.Mode
		files map[string]string
		want  map[string]string
	}{
		{
			name:  "default mode",
			mode:  llm.DefaultMode,
			files: map[string]string{"CLAUDE.md": "\n"},
			want:  map[string]string{"AGENTS.md": testBlock + "\n## This repository\n", "CLAUDE.md": "@AGENTS.md\n"},
		},
		{
			name:  "migration mode moves the file with content",
			mode:  llm.MigrationMode,
			files: map[string]string{"CLAUDE.md": " \n", ".claude/CLAUDE.md": "Claude only.\n"},
			want:  map[string]string{"AGENTS.md": testBlock + "\nClaude only.\n", "CLAUDE.md": "@AGENTS.md\n"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := newCheckout(t)
			writeFiles(t, root, tc.files)

			apply(t, root, tc.mode)
			require.Equal(t, tc.want, readTree(t, root))
		})
	}
}

func TestChanges_AgentsChangedSincePlan(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"edited":  {"AGENTS.md": "<!-- BEGIN devctl-managed -->\n- old\n<!-- END devctl-managed -->\n"},
		"created": {},
	} {
		t.Run(name, func(t *testing.T) {
			root := newCheckout(t)
			writeFiles(t, root, files)

			changes, _, err := llm.Plan(t.Context(), root, llm.DefaultMode, testBody)
			require.NoError(t, err)
			writeFiles(t, root, map[string]string{"AGENTS.md": "Local, edited.\n"})
			before := readTree(t, root)

			require.ErrorContains(t, changes.Apply(t.Context()), "AGENTS.md changed")
			require.Equal(t, before, readTree(t, root))
		})
	}
}
