package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/file"
)

// Migration moves a repository's existing agent instructions and skills to
// AGENTS.md and .agents/skills. Claude instructions with no AGENTS.md to go
// with are the repository's instructions: their content moves to AGENTS.md.
// A root CLAUDE.md is left as an import of AGENTS.md, a .claude/CLAUDE.md is
// deleted. Claude files that keep their own content import AGENTS.md.
type Migration struct {
	root string

	// agents is the content of an existing AGENTS.md with instructions.
	agents []byte
	// source is the Claude file whose content moves to AGENTS.md.
	source        string
	sourceContent []byte
	// importers are the Claude files that keep their content and need an
	// import of AGENTS.md, with that content.
	importers        []string
	importerContents map[string][]byte

	// skills are the entries of a .claude/skills directory to move to
	// .agents/skills; links to a shared skill are removed instead.
	skills []skillEntry
}

type skillEntry struct {
	name   string
	isLink bool
}

// agentsImports is the import of the root AGENTS.md from each Claude file.
var agentsImports = map[string]string{
	claudeFile:    agentsFile,
	dotClaudeFile: "../" + agentsFile,
}

// PlanMigration plans the migration of the repository at root. A repository
// with findings gets no migration.
func PlanMigration(ctx context.Context, root string) (Migration, Report, error) {
	report, err := Preflight(ctx, root, MigrationMode)
	if err != nil || len(report.Findings) > 0 {
		return Migration{}, report, microerror.Mask(err)
	}

	m := Migration{root: root, importerContents: map[string][]byte{}}
	p := &preflight{root: root}

	agents, err := p.readRegular(agentsFile)
	if err != nil {
		return Migration{}, Report{}, microerror.Mask(err)
	}
	if len(bytes.TrimSpace(agents)) > 0 {
		m.agents = agents
	}

	for _, name := range []string{claudeFile, dotClaudeFile} {
		content, err := p.readRegular(name)
		if err != nil {
			return Migration{}, Report{}, microerror.Mask(err)
		}
		switch {
		case content == nil:
		case m.agents == nil && m.source == "":
			m.source, m.sourceContent = name, content
		case !bytes.Equal(file.AddImport(content, agentsImports[name]), content):
			m.importers = append(m.importers, name)
			m.importerContents[name] = content
		}
	}

	info, err := p.lstat(claudeSkillsDir)
	if err != nil {
		return Migration{}, Report{}, microerror.Mask(err)
	}
	if info != nil && info.IsDir() {
		names, err := p.list(claudeSkillsDir)
		if err != nil {
			return Migration{}, Report{}, microerror.Mask(err)
		}
		for _, name := range names {
			info, err := p.lstat(path.Join(claudeSkillsDir, name))
			if err != nil {
				return Migration{}, Report{}, microerror.Mask(err)
			}
			m.skills = append(m.skills, skillEntry{name: name, isLink: info.Mode()&fs.ModeSymlink != 0})
		}
		if len(names) == 0 {
			// An empty directory still becomes the link.
			m.skills = []skillEntry{}
		}
	}

	return m, Report{}, nil
}

// Steps describes the changes Apply makes.
func (m Migration) Steps() []string {
	if m.root == "" {
		return nil
	}
	var steps []string
	switch {
	case m.source != "":
		steps = append(steps, fmt.Sprintf("move the content of %s to AGENTS.md, below the devctl-managed block", m.source))
		if m.source == claudeFile {
			steps = append(steps, "replace CLAUDE.md with an import of AGENTS.md")
		} else {
			steps = append(steps, fmt.Sprintf("delete %s", m.source))
		}
	case m.agents != nil:
		steps = append(steps, "add or update the devctl-managed block in AGENTS.md")
	default:
		steps = append(steps, "create AGENTS.md with the devctl-managed block")
	}
	for _, name := range m.importers {
		steps = append(steps, fmt.Sprintf("add an import of AGENTS.md to %s", name))
	}
	for _, s := range m.skills {
		from, to := path.Join(claudeSkillsDir, s.name), path.Join(agentsSkillsDir, s.name)
		if s.isLink {
			steps = append(steps, fmt.Sprintf("remove %s, a link to %s", from, to))
		} else {
			steps = append(steps, fmt.Sprintf("move %s to %s", from, to))
		}
	}
	if m.skills != nil {
		steps = append(steps, fmt.Sprintf("replace %s with a link to %s", claudeSkillsDir, claudeSkillsTarget))
	}
	return steps
}

// Apply migrates the repository, with body as the content of the
// devctl-managed block. All content is prepared before the first write, and
// a source changes only after its content was written to AGENTS.md.
func (m Migration) Apply(body string) error {
	if m.root == "" {
		return microerror.Maskf(executionFailedError, "the migration was not planned or has findings")
	}
	base := m.agents
	if m.source != "" {
		base = file.RemoveImport(m.sourceContent, agentsImports[m.source])
		if dir := path.Dir(m.source); dir != "." {
			base = file.RebaseImports(base, dir, m.exists)
		}
	}
	agents, err := file.MergeManagedBlock(base, body)
	if err != nil {
		return microerror.Mask(err)
	}
	importers := map[string][]byte{}
	for _, name := range m.importers {
		importers[name] = file.AddImport(m.importerContents[name], agentsImports[name])
	}

	if err := m.write(agentsFile, agents); err != nil {
		return microerror.Mask(err)
	}
	for _, name := range m.importers {
		if err := m.write(name, importers[name]); err != nil {
			return microerror.Mask(err)
		}
	}
	switch m.source {
	case claudeFile:
		if err := m.write(claudeFile, []byte("@"+agentsFile+"\n")); err != nil {
			return microerror.Mask(err)
		}
	case dotClaudeFile:
		if err := os.Remove(m.path(dotClaudeFile)); err != nil {
			return microerror.Mask(err)
		}
	}

	return microerror.Mask(m.moveSkills())
}

// moveSkills moves the skill directories to .agents/skills and replaces
// .claude/skills with a link to it.
func (m Migration) moveSkills() error {
	if m.skills == nil {
		return nil
	}
	if err := os.MkdirAll(m.path(agentsSkillsDir), 0o755); err != nil { // #nosec G301 -- a directory of the repository
		return microerror.Mask(err)
	}
	for _, s := range m.skills {
		from := m.path(path.Join(claudeSkillsDir, s.name))
		if s.isLink {
			if err := os.Remove(from); err != nil {
				return microerror.Mask(err)
			}
			continue
		}
		if err := os.Rename(from, m.path(path.Join(agentsSkillsDir, s.name))); err != nil {
			return microerror.Mask(err)
		}
	}
	if err := os.Remove(m.path(claudeSkillsDir)); err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(os.Symlink(claudeSkillsTarget, m.path(claudeSkillsDir)))
}

// write replaces name with content through a temporary file in the same
// directory, keeping the mode of an existing file. Unchanged content is not
// written.
func (m Migration) write(name string, content []byte) error {
	target := m.path(name)
	mode := fs.FileMode(0o644)
	info, err := os.Stat(target)
	switch {
	case err == nil:
		mode = info.Mode().Perm()
		existing, err := os.ReadFile(target) // #nosec G304 -- a fixed output name inside the repository
		if err != nil {
			return microerror.Mask(err)
		}
		if bytes.Equal(existing, content) {
			return nil
		}
	case !errors.Is(err, fs.ErrNotExist):
		return microerror.Mask(err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*")
	if err != nil {
		return microerror.Mask(err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return microerror.Mask(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return microerror.Mask(err)
	}
	if err := tmp.Close(); err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(os.Rename(tmp.Name(), target))
}

func (m Migration) exists(rel string) bool {
	_, err := os.Stat(m.path(rel))
	return err == nil
}

func (m Migration) path(name string) string {
	return filepath.Join(m.root, filepath.FromSlash(name))
}
