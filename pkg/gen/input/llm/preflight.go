package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/file"
)

// Mode selects what gen llm may change in a repository.
type Mode int

const (
	// DefaultMode handles repositories without instructions and repositories
	// whose AGENTS.md has a devctl-managed block. Alignment runs in this mode.
	DefaultMode Mode = iota
	// MigrationMode also moves existing instructions and skills.
	MigrationMode
)

// Finding is a reason to leave a repository unchanged.
type Finding struct {
	// Path is relative to the repository root, with slashes.
	Path    string
	Problem string
	Fix     string
}

// Report lists the findings of a preflight. A repository with findings must
// not be changed.
type Report struct {
	Findings []Finding
}

// inspect checks the repository at root and records its layout.
func inspect(ctx context.Context, root string, mode Mode) (*preflight, error) {
	p := &preflight{root: root, mode: mode, claudeContents: map[string][]byte{}}
	if err := p.run(ctx); err != nil {
		return nil, microerror.Mask(err)
	}
	slices.SortStableFunc(p.report.Findings, func(a, b Finding) int { return strings.Compare(a.Path, b.Path) })
	return p, nil
}

const (
	agentsFile      = "AGENTS.md"
	claudeFile      = "CLAUDE.md"
	claudeDir       = ".claude"
	agentsDir       = ".agents"
	dotClaudeFile   = claudeDir + "/" + claudeFile
	agentsSkillsDir = agentsDir + "/skills"
	claudeSkillsDir = claudeDir + "/skills"

	// claudeSkillsTarget is where .claude/skills links to.
	claudeSkillsTarget = "../" + agentsSkillsDir

	migrateFix = "run devctl gen llm --migrate-instructions and review the result"
)

type preflight struct {
	root   string
	mode   Mode
	report Report
	// names caches directory listings, by directory.
	names map[string][]string

	// The layout found, for planning a migration.
	agentsState   agentsState
	agentsContent []byte
	// agentsRaw is the content of a regular AGENTS.md, also an empty one.
	agentsRaw      []byte
	agentsExists   bool
	claudeContents map[string][]byte
	// source is the Claude file whose content moves to a missing AGENTS.md.
	source       string
	claudeSkills dirState
	// claudeSkillsExists is true for any .claude/skills, also a link.
	claudeSkillsExists bool
	sharedSkills       dirState
	skillEntries       []skillEntry
}

// instructionFiles are the root instruction files gen llm handles.
var instructionFiles = []string{agentsFile, claudeFile, dotClaudeFile}

// knownNames are the names gen llm uses in each directory, to find variants
// that differ only in case.
var knownNames = map[string][]string{
	".":       {agentsFile, claudeFile, claudeDir, agentsDir},
	claudeDir: {claudeFile, "skills"},
	agentsDir: {"skills"},
}

func (p *preflight) run(ctx context.Context) error {
	if err := p.checkCaseVariants("."); err != nil {
		return microerror.Mask(err)
	}
	claude, err := p.checkDir(claudeDir)
	if err != nil {
		return microerror.Mask(err)
	}
	agents, err := p.checkDir(agentsDir)
	if err != nil {
		return microerror.Mask(err)
	}
	sharedSkills := dirRejected
	if agents != dirRejected {
		sharedSkills = dirAbsent
	}
	if agents == dirPresent {
		if err := p.checkCaseVariants(agentsDir); err != nil {
			return microerror.Mask(err)
		}
		if sharedSkills, err = p.checkDir(agentsSkillsDir); err != nil {
			return microerror.Mask(err)
		}
	}
	p.sharedSkills = sharedSkills
	if claude == dirPresent {
		if err := p.checkCaseVariants(claudeDir); err != nil {
			return microerror.Mask(err)
		}
		if err := p.checkClaudeSkills(sharedSkills); err != nil {
			return microerror.Mask(err)
		}
	}

	agentsFileState, err := p.checkAgentsFile()
	if err != nil {
		return microerror.Mask(err)
	}
	p.agentsState = agentsFileState

	var instructions []string
	if agentsFileState != agentsAbsent {
		instructions = append(instructions, agentsFile)
	}
	claudeFiles := []string{claudeFile}
	if claude == dirPresent {
		claudeFiles = append(claudeFiles, dotClaudeFile)
	}
	for _, name := range claudeFiles {
		holds, err := p.checkClaudeFile(name)
		if err != nil {
			return microerror.Mask(err)
		}
		if holds {
			instructions = append(instructions, name)
		}
	}

	if p.mode == MigrationMode && agentsFileState == agentsAbsent {
		if err := p.checkSource(); err != nil {
			return microerror.Mask(err)
		}
	}

	// Git refuses to look beyond a link, and a rejected directory is
	// reported already.
	outputs := []ignorable{{query: agentsFile}}
	if claude != dirRejected {
		outputs = append(outputs, ignorable{query: claudeSkillsDir})
	}
	if sharedSkills != dirRejected {
		outputs = append(outputs,
			// The trailing slash makes Git match the path as a directory.
			ignorable{query: agentsSkillsDir + "/", path: agentsSkillsDir},
			ignorable{query: agentsSkillsDir + "/skill/SKILL.md", path: agentsSkillsDir, problem: "would hold SKILL.md files that Git ignores, so a commit with git add -A would leave them out"},
		)
	}
	if err := p.checkIgnored(ctx, outputs); err != nil {
		return microerror.Mask(err)
	}

	// Migrating an AGENTS.md that cannot be used would not help.
	needsMigration := (agentsFileState == agentsAbsent || agentsFileState == agentsWithoutBlock) && len(instructions) > 0
	if p.mode == DefaultMode && needsMigration {
		p.add(instructions[0], "holds instructions that are not in a devctl-managed block yet", migrateFix)
	}
	return nil
}

type dirState int

const (
	dirAbsent dirState = iota
	dirPresent
	// dirRejected is a path that gen llm cannot use as a directory.
	dirRejected
)

// checkSource picks the Claude file whose content moves to the missing
// AGENTS.md, and reports devctl-managed markers in it that the merge cannot
// handle.
func (p *preflight) checkSource() error {
	for _, name := range []string{claudeFile, dotClaudeFile} {
		content, ok := p.claudeContents[name]
		if !ok || isBlank(content) {
			continue
		}
		p.source = name
		if _, problem := file.CheckManagedBlock(content); problem != "" {
			p.add(name, problem, file.ManagedBlockFix)
		}
		return nil
	}
	return nil
}

// isBlank reports whether content holds nothing but whitespace, which the
// merge treats like a missing file.
func isBlank(content []byte) bool {
	return len(bytes.TrimSpace(content)) == 0
}

// checkDir reports a directory that gen llm writes into but that is a link or
// not a directory.
func (p *preflight) checkDir(name string) (dirState, error) {
	info, err := p.lstat(name)
	if err != nil || info == nil {
		return dirAbsent, microerror.Mask(err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		p.add(name, "is a link, so gen llm would change files outside it", "replace the link with a directory")
	case !info.IsDir():
		p.add(name, "is not a directory", "remove or rename it")
	default:
		return dirPresent, nil
	}
	return dirRejected, nil
}

// checkClaudeSkills checks that .claude/skills is missing or a link to the
// shared skills. A directory is moved there in migration mode, as long as
// every entry is a skill directory without a shared counterpart or a link to
// the shared skill of the same name.
func (p *preflight) checkClaudeSkills(sharedSkills dirState) error {
	info, err := p.lstat(claudeSkillsDir)
	if err != nil || info == nil {
		return microerror.Mask(err)
	}
	p.claudeSkillsExists = true
	if info.IsDir() {
		p.claudeSkills = dirPresent
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(p.path(claudeSkillsDir))
		if err != nil {
			return microerror.Mask(err)
		}
		if path.Clean(filepath.ToSlash(target)) != claudeSkillsTarget {
			p.add(claudeSkillsDir, fmt.Sprintf("is a link to %s", target), fmt.Sprintf("move the skills to %s and replace the link with one to %s", agentsSkillsDir, claudeSkillsTarget))
		}
		return nil
	case !info.IsDir():
		p.add(claudeSkillsDir, "is not a directory", "remove or rename it")
		return nil
	case p.mode == DefaultMode:
		p.add(claudeSkillsDir, fmt.Sprintf("is a directory, and its skills belong in %s", agentsSkillsDir), migrateFix)
		return nil
	}

	names, err := p.list(claudeSkillsDir)
	if err != nil {
		return microerror.Mask(err)
	}
	var sharedNames []string
	if sharedSkills == dirPresent {
		if sharedNames, err = p.list(agentsSkillsDir); err != nil {
			return microerror.Mask(err)
		}
	}
	for _, name := range names {
		entry := path.Join(claudeSkillsDir, name)
		shared := path.Join(agentsSkillsDir, name)
		info, err := p.lstat(entry)
		if err != nil {
			return microerror.Mask(err)
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p.path(entry))
			if err != nil {
				return microerror.Mask(err)
			}
			if path.Join(claudeSkillsDir, filepath.ToSlash(target)) != shared {
				p.add(entry, fmt.Sprintf("is a link to %s", target), fmt.Sprintf("replace it with the skill directory or a link to ../../%s", shared))
			}
			p.skillEntries = append(p.skillEntries, skillEntry{name: name, isLink: true})
		case !info.IsDir():
			fix := "remove it"
			if !strings.HasPrefix(name, ".") {
				fix = fmt.Sprintf("move it to %s/SKILL.md or remove it", strings.TrimSuffix(entry, path.Ext(name)))
			}
			p.add(entry, "is a file, not a skill directory", fix)
		default:
			// On a case-insensitive file system, the move would merge them.
			i := slices.IndexFunc(sharedNames, func(n string) bool { return strings.EqualFold(n, name) })
			if i >= 0 {
				p.add(entry, fmt.Sprintf("has the same name as %s", path.Join(agentsSkillsDir, sharedNames[i])), "keep only one of the two skills")
			}
			p.skillEntries = append(p.skillEntries, skillEntry{name: name})
		}
	}
	return nil
}

// ignorable is an output to ask git check-ignore about.
type ignorable struct {
	query string
	// path and problem are reported, by default query and that it is ignored.
	path, problem string
}

// checkIgnored reports outputs that Git would ignore. Alignment commits with
// git add -A, which skips them, so it would commit only part of the change.
// Outside a Git checkout there is nothing to check.
func (p *preflight) checkIgnored(ctx context.Context, outputs []ignorable) error {
	args := []string{"check-ignore", "--"}
	for _, o := range outputs {
		args = append(args, o.query)
	}
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed arguments, without a shell
	cmd.Dir = p.root
	// Git's messages are matched below.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return nil
	case errors.As(err, &exitErr) && strings.Contains(string(exitErr.Stderr), "not a git repository"):
		return nil
	case errors.As(err, &exitErr):
		return microerror.Maskf(executionFailedError, "git check-ignore: %s: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	case err != nil:
		return microerror.Maskf(executionFailedError, "git check-ignore: %s", err)
	}

	reported := map[string]bool{}
	for line := range strings.Lines(string(out)) {
		query := strings.TrimSpace(line)
		i := slices.IndexFunc(outputs, func(o ignorable) bool { return o.query == query })
		if i < 0 {
			continue
		}
		o := outputs[i]
		if o.path == "" {
			o.path = query
		}
		if o.problem == "" {
			o.problem = "is ignored by Git, so a commit with git add -A would leave it out"
		}
		if reported[o.path] {
			continue
		}
		reported[o.path] = true
		p.add(o.path, o.problem, "remove the entry that matches it from .gitignore, as shown by git check-ignore -v "+query)
	}
	return nil
}

type agentsState int

const (
	agentsAbsent agentsState = iota
	agentsWithoutBlock
	agentsWithBlock
	// agentsUnusable is an AGENTS.md that gen llm must not write.
	agentsUnusable
)

// checkAgentsFile reports a root AGENTS.md that gen llm cannot merge the
// devctl-managed block into, and returns its state.
func (p *preflight) checkAgentsFile() (agentsState, error) {
	info, err := p.lstat(agentsFile)
	if err != nil || info == nil {
		return agentsAbsent, microerror.Mask(err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(p.path(agentsFile))
		if err != nil {
			return agentsUnusable, microerror.Mask(err)
		}
		p.add(agentsFile, fmt.Sprintf("is a link to %s, so the devctl-managed block would be written into that file", target),
			fmt.Sprintf("replace the link with a regular AGENTS.md, for example one that refers to %s", target))
		return agentsUnusable, nil
	}
	if !info.Mode().IsRegular() {
		p.add(agentsFile, "is not a regular file", "remove or rename it")
		return agentsUnusable, nil
	}
	content, err := os.ReadFile(p.path(agentsFile))
	if err != nil {
		return agentsUnusable, microerror.Mask(err)
	}
	p.agentsRaw, p.agentsExists = content, true
	if isBlank(content) {
		return agentsAbsent, nil
	}
	hasBlock, problem := file.CheckManagedBlock(content)
	switch {
	case problem != "":
		p.add(agentsFile, problem, file.ManagedBlockFix)
		return agentsUnusable, nil
	case hasBlock:
		p.agentsContent = content
		return agentsWithBlock, nil
	}
	p.agentsContent = content
	return agentsWithoutBlock, nil
}

// checkClaudeFile reports a Claude instruction file that is neither a regular
// file nor a link to the root AGENTS.md, and returns whether it is a regular
// file, which holds instructions of its own.
func (p *preflight) checkClaudeFile(name string) (bool, error) {
	info, err := p.lstat(name)
	if err != nil || info == nil {
		return false, microerror.Mask(err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(p.path(name))
		if err != nil {
			return false, microerror.Mask(err)
		}
		if filepath.IsAbs(target) || path.Join(path.Dir(name), filepath.ToSlash(target)) != agentsFile {
			p.add(name, fmt.Sprintf("is a link to %s", target), "replace the link with a regular file or a link to AGENTS.md")
		}
		return false, nil
	case !info.Mode().IsRegular():
		p.add(name, "is not a regular file", "remove or rename it")
		return false, nil
	}
	content, err := os.ReadFile(p.path(name))
	if err != nil {
		return false, microerror.Mask(err)
	}
	p.claudeContents[name] = content
	return !isBlank(content), nil
}

func (p *preflight) checkCaseVariants(dir string) error {
	names, err := p.list(dir)
	if err != nil {
		return microerror.Mask(err)
	}
	for _, name := range names {
		for _, known := range knownNames[dir] {
			if name != known && strings.EqualFold(name, known) {
				p.add(path.Join(dir, name), fmt.Sprintf("differs from %s only in case", known), fmt.Sprintf("rename it to %s", known))
			}
		}
	}
	return nil
}

// list returns the names in dir, or none for a missing directory.
func (p *preflight) list(dir string) ([]string, error) {
	if names, ok := p.names[dir]; ok {
		return names, nil
	}
	entries, err := os.ReadDir(p.path(dir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, microerror.Mask(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	if p.names == nil {
		p.names = map[string][]string{}
	}
	p.names[dir] = names
	return names, nil
}

// lstat returns nil information for a missing path. A name counts as missing
// unless its directory lists it exactly, because a case-insensitive file
// system also finds names that differ in case.
func (p *preflight) lstat(name string) (fs.FileInfo, error) {
	names, err := p.list(path.Dir(name))
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if !slices.Contains(names, path.Base(name)) {
		return nil, nil
	}
	info, err := os.Lstat(p.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return info, microerror.Mask(err)
}

func (p *preflight) path(name string) string {
	return filepath.Join(p.root, filepath.FromSlash(name))
}

func (p *preflight) add(path, problem, fix string) {
	p.report.Findings = append(p.report.Findings, Finding{Path: path, Problem: problem, Fix: fix})
}
