package llm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/file"
)

// Migration moves a repository's existing agent instructions and skills to
// AGENTS.md and .agents/skills. Claude instructions with no AGENTS.md to go
// with are the repository's instructions: their content moves to AGENTS.md.
// A root CLAUDE.md is left as an import of AGENTS.md, a .claude/CLAUDE.md is
// deleted. Claude files that keep their own content import AGENTS.md.
type Migration struct {
	root    string
	planned bool

	// agents is the content of an existing AGENTS.md with instructions.
	agents []byte
	// source is the Claude file whose content moves to AGENTS.md.
	source *plannedFile
	// importers are the Claude files that keep their content and gain an
	// import of AGENTS.md.
	importers []plannedFile

	// skills are the entries of a .claude/skills directory to move to
	// .agents/skills; links to a shared skill are removed instead.
	skills []skillEntry
	// linkSkills replaces the .claude/skills directory with a link.
	linkSkills bool
}

// plannedFile is a file the migration changes, with its content when it was
// planned and the content it gets.
type plannedFile struct {
	name          string
	before, after []byte
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
	p, err := inspect(ctx, root, MigrationMode)
	if err != nil {
		return Migration{}, Report{}, microerror.Mask(err)
	}
	if len(p.report.Findings) > 0 {
		return Migration{}, p.report, nil
	}

	m := Migration{root: root, planned: true, agents: p.agentsContent}
	for _, name := range []string{claudeFile, dotClaudeFile} {
		content, ok := p.claudeContents[name]
		switch {
		case !ok:
		case name == p.source:
			m.source = &plannedFile{name: name, before: content}
		default:
			if after := file.AddImport(content, agentsImports[name]); !bytes.Equal(after, content) {
				m.importers = append(m.importers, plannedFile{name: name, before: content, after: after})
			}
		}
	}
	if p.claudeSkills == dirPresent {
		m.skills, m.linkSkills = p.skillEntries, true
	}
	return m, Report{}, nil
}

// Steps describes the changes Apply makes.
func (m Migration) Steps() []string {
	if !m.planned {
		return nil
	}
	var steps []string
	switch {
	case m.source != nil:
		steps = append(steps, fmt.Sprintf("move the content of %s to AGENTS.md, below the devctl-managed block", m.source.name))
		if m.source.name == claudeFile {
			steps = append(steps, "replace CLAUDE.md with an import of AGENTS.md")
		} else {
			steps = append(steps, fmt.Sprintf("delete %s", m.source.name))
		}
	case m.agents != nil:
		steps = append(steps, "add or update the devctl-managed block in AGENTS.md")
	default:
		steps = append(steps, "create AGENTS.md with the devctl-managed block")
	}
	for _, f := range m.importers {
		steps = append(steps, fmt.Sprintf("add an import of AGENTS.md to %s", f.name))
	}
	for _, s := range m.skills {
		from, to := path.Join(claudeSkillsDir, s.name), path.Join(agentsSkillsDir, s.name)
		if s.isLink {
			steps = append(steps, fmt.Sprintf("remove %s, a link to %s", from, to))
		} else {
			steps = append(steps, fmt.Sprintf("move %s to %s", from, to))
		}
	}
	if m.linkSkills {
		steps = append(steps, fmt.Sprintf("replace %s with a link to %s", claudeSkillsDir, claudeSkillsTarget))
	}
	return steps
}

// Apply migrates the repository, with body as the content of the
// devctl-managed block. It stops before the first write if a file it changes
// is no longer as planned or the content cannot be prepared, and writes
// AGENTS.md before it changes the file whose content moved there.
func (m Migration) Apply(ctx context.Context, body string) error {
	if !m.planned {
		return microerror.Maskf(executionFailedError, "the migration was not planned or has findings")
	}
	planned := m.importers
	if m.source != nil {
		planned = append([]plannedFile{*m.source}, planned...)
	}
	for _, f := range planned {
		current, err := os.ReadFile(m.path(f.name))
		if err != nil {
			return microerror.Mask(err)
		}
		if !bytes.Equal(current, f.before) {
			return microerror.Maskf(executionFailedError, "%s changed since the migration was planned", f.name)
		}
	}

	base := m.agents
	if m.source != nil {
		base = file.MoveInstructions(m.source.before, path.Dir(m.source.name), m.exists, instructionFiles)
	}
	agents, err := file.MergeManagedBlock(base, body)
	if err != nil {
		return microerror.Mask(err)
	}

	inputs := []input.Input{m.output(agentsFile, agents)}
	for _, f := range m.importers {
		inputs = append(inputs, m.output(f.name, f.after))
	}
	if m.source != nil {
		if m.source.name == claudeFile {
			inputs = append(inputs, m.output(claudeFile, []byte("@"+agentsFile+"\n")))
		} else {
			inputs = append(inputs, input.Input{Path: m.path(m.source.name), Delete: true})
		}
	}
	if err := gen.Execute(ctx, inputs...); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(m.moveSkills())
}

func (m Migration) output(name string, content []byte) input.Input {
	return input.Input{
		Path:           m.path(name),
		SkipRegenCheck: true,
		Generate:       func(context.Context) ([]byte, error) { return content, nil },
	}
}

// moveSkills moves the skill directories to .agents/skills and replaces
// .claude/skills with a link to it.
func (m Migration) moveSkills() error {
	if !m.linkSkills {
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

func (m Migration) exists(rel string) bool {
	_, err := os.Stat(m.path(rel))
	return err == nil
}

func (m Migration) path(name string) string {
	return filepath.Join(m.root, filepath.FromSlash(name))
}
