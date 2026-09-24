package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestTreeEntries_Symlink pins that a symlink in the rendered scaffold
// becomes a tree entry with mode 120000 whose content is the link's target,
// never the file it points at -- what pushScaffold needs so the commit the
// Git Data API builds carries the symlink itself.
func TestTreeEntries_Symlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "skills", "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "skills"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", ".agents", "skills", "x"), filepath.Join(dir, ".claude", "skills", "x")); err != nil {
		t.Fatal(err)
	}

	r := &Runner{}
	s := &run{owner: "acme", name: "repo"}
	files := []string{"AGENTS.md", "CLAUDE.md", ".claude/skills/x"}

	entries, err := r.treeEntries(context.Background(), s, dir, files)
	if err != nil {
		t.Fatalf("treeEntries: %v", err)
	}
	if len(entries) != len(files) {
		t.Fatalf("got %d entries, want %d", len(entries), len(files))
	}

	byPath := map[string]int{}
	for i, e := range entries {
		byPath[e.GetPath()] = i
	}

	agents := entries[byPath["AGENTS.md"]]
	if agents.GetMode() != "100644" {
		t.Errorf("AGENTS.md mode = %s, want 100644", agents.GetMode())
	}
	if agents.GetContent() != "agents" {
		t.Errorf("AGENTS.md content = %q, want %q", agents.GetContent(), "agents")
	}

	claude := entries[byPath["CLAUDE.md"]]
	if claude.GetMode() != "120000" {
		t.Errorf("CLAUDE.md mode = %s, want 120000", claude.GetMode())
	}
	if claude.GetContent() != "AGENTS.md" {
		t.Errorf("CLAUDE.md content = %q, want %q (its link target, not AGENTS.md's own content)", claude.GetContent(), "AGENTS.md")
	}

	skill := entries[byPath[".claude/skills/x"]]
	if skill.GetMode() != "120000" {
		t.Errorf(".claude/skills/x mode = %s, want 120000", skill.GetMode())
	}
	wantTarget := filepath.Join("..", "..", ".agents", "skills", "x")
	if skill.GetContent() != wantTarget {
		t.Errorf(".claude/skills/x content = %q, want %q", skill.GetContent(), wantTarget)
	}
}
