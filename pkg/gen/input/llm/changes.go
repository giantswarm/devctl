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

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/file"
)

// Changes brings a repository's agent instructions to the devctl-managed
// block in AGENTS.md. In migration mode, it also moves existing instructions
// and skills: Claude instructions with no AGENTS.md to go with are the
// repository's instructions, so their content moves to AGENTS.md. A root
// CLAUDE.md is left as an import of AGENTS.md, a .claude/CLAUDE.md is
// deleted. Claude files that keep their own content import AGENTS.md.
type Changes struct {
	root    string
	planned bool

	// agents is the content of an existing AGENTS.md with instructions, and
	// agentsAfter the content it gets.
	agents, agentsAfter []byte
	// agentsBefore is the content of AGENTS.md when the changes were
	// planned, and agentsExisted whether there was one.
	agentsBefore   []byte
	agentsExisted  bool
	agentsHadBlock bool
	writeAgents    bool
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
	// createSkillsLink creates the missing .claude/skills link to existing
	// shared skills.
	createSkillsLink bool

	// legacy are the generated rules that AGENTS.md replaces.
	legacy []string
}

// legacyRules are the files that gen llm generated before AGENTS.md, and
// legacyDirs their directories, removed once empty.
var (
	legacyRules = []string{".cursor/rules/zz_generated.base-llm-rules.mdc", ".cursor/rules/zz_generated.go-llm-rules.mdc"}
	legacyDirs  = []string{".cursor/rules", ".cursor"}
)

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

// Plan plans the changes to the repository at root in mode, with body as the
// content of the devctl-managed block. A repository with findings gets no
// changes. All new content is prepared here, before anything is written.
func Plan(ctx context.Context, root string, mode Mode, body string) (Changes, Report, error) {
	p, err := inspect(ctx, root, mode)
	if err != nil {
		return Changes{}, Report{}, microerror.Mask(err)
	}
	if len(p.report.Findings) > 0 {
		return Changes{}, p.report, nil
	}

	m := Changes{
		root:           root,
		planned:        true,
		agents:         p.agentsContent,
		agentsBefore:   p.agentsRaw,
		agentsExisted:  p.agentsExists,
		agentsHadBlock: p.agentsState == agentsWithBlock,
	}
	for _, name := range []string{claudeFile, dotClaudeFile} {
		content, ok := p.claudeContents[name]
		if !ok {
			continue
		}
		if name == p.source {
			m.source = &plannedFile{name: name, before: content}
			continue
		}
		// A file without instructions of its own becomes the import alone.
		after := file.AddImport(content, agentsImports[name])
		if isBlank(content) {
			after = []byte("@" + agentsImports[name] + "\n")
		}
		if !bytes.Equal(after, content) {
			m.importers = append(m.importers, plannedFile{name: name, before: content, after: after})
		}
	}
	if p.claudeSkills == dirPresent {
		m.skills, m.linkSkills = p.skillEntries, true
	}
	base := m.agents
	if m.source != nil {
		base = file.MoveInstructions(m.source.before, path.Dir(m.source.name), m.exists, instructionFiles)
	}
	if m.agentsAfter, err = file.MergeManagedBlock(base, body); err != nil {
		return Changes{}, Report{}, microerror.Mask(err)
	}
	m.writeAgents = !m.agentsExisted || !bytes.Equal(m.agentsAfter, m.agentsBefore)

	m.createSkillsLink = p.sharedSkills == dirPresent && !p.claudeSkillsExists
	if m.realDir(legacyDirs[1]) && m.realDir(legacyDirs[0]) {
		for _, name := range legacyRules {
			if info, err := os.Lstat(m.path(name)); err == nil && info.Mode().IsRegular() {
				m.legacy = append(m.legacy, name)
			}
		}
	}
	return m, Report{}, nil
}

// Steps describes the changes Apply makes.
func (m Changes) Steps() []string {
	if !m.planned {
		return nil
	}
	var steps []string
	switch {
	case !m.writeAgents:
	case m.source != nil:
		steps = append(steps, fmt.Sprintf("move the content of %s to AGENTS.md, below the devctl-managed block", m.source.name))
		if m.source.name == claudeFile {
			steps = append(steps, "replace CLAUDE.md with an import of AGENTS.md")
		} else {
			steps = append(steps, fmt.Sprintf("delete %s", m.source.name))
		}
	case m.agentsHadBlock:
		steps = append(steps, "update the devctl-managed block in AGENTS.md")
	case m.agents != nil:
		steps = append(steps, "add the devctl-managed block to AGENTS.md")
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
	if m.createSkillsLink {
		steps = append(steps, fmt.Sprintf("create %s as a link to %s", claudeSkillsDir, claudeSkillsTarget))
	}
	for _, name := range m.legacy {
		steps = append(steps, fmt.Sprintf("delete %s", name))
	}
	return steps
}

// Apply makes the planned changes. It stops before the first write if a file
// it changes is no longer as planned, and writes AGENTS.md before it changes
// the file whose content moved there.
func (m Changes) Apply(ctx context.Context) error {
	if !m.planned {
		return microerror.Maskf(executionFailedError, "the changes were not planned or have findings")
	}
	planned := m.importers
	if m.source != nil {
		planned = append([]plannedFile{*m.source}, planned...)
	}
	current, err := os.ReadFile(m.path(agentsFile))
	switch {
	case errors.Is(err, fs.ErrNotExist) && !m.agentsExisted:
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return microerror.Mask(err)
	case err != nil || !m.agentsExisted || !bytes.Equal(current, m.agentsBefore):
		return microerror.Maskf(executionFailedError, "%s changed since the changes were planned", agentsFile)
	}
	for _, f := range planned {
		current, err := os.ReadFile(m.path(f.name))
		if err != nil {
			return microerror.Mask(err)
		}
		if !bytes.Equal(current, f.before) {
			return microerror.Maskf(executionFailedError, "%s changed since the changes were planned", f.name)
		}
	}

	var inputs []input.Input
	if m.writeAgents {
		inputs = append(inputs, m.output(agentsFile, m.agentsAfter))
	}
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
	if err := m.moveSkills(); err != nil {
		return microerror.Mask(err)
	}
	if m.createSkillsLink {
		if err := os.MkdirAll(m.path(claudeDir), 0o755); err != nil { // #nosec G301 -- a directory of the repository
			return microerror.Mask(err)
		}
		if err := os.Symlink(claudeSkillsTarget, m.path(claudeSkillsDir)); err != nil {
			return microerror.Mask(err)
		}
	}

	// The legacy rules go last, once their replacement is in place. Their
	// directories go only if nothing else is left in them.
	var deletions []input.Input
	for _, name := range m.legacy {
		deletions = append(deletions, input.Input{Path: m.path(name), Delete: true})
	}
	if len(m.legacy) > 0 {
		for _, dir := range legacyDirs {
			deletions = append(deletions, input.Input{Path: m.path(dir), Delete: true})
		}
	}
	return microerror.Mask(gen.Execute(ctx, deletions...))
}

func (m Changes) output(name string, content []byte) input.Input {
	return input.Input{
		Path:           m.path(name),
		SkipRegenCheck: true,
		Generate:       func(context.Context) ([]byte, error) { return content, nil },
	}
}

// moveSkills moves the skill directories to .agents/skills and replaces
// .claude/skills with a link to it.
func (m Changes) moveSkills() error {
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

// realDir reports whether name is a directory, not a link to one.
func (m Changes) realDir(name string) bool {
	info, err := os.Lstat(m.path(name))
	return err == nil && info.IsDir()
}

func (m Changes) exists(rel string) bool {
	_, err := os.Stat(m.path(rel))
	return err == nil
}

func (m Changes) path(name string) string {
	return filepath.Join(m.root, filepath.FromSlash(name))
}
