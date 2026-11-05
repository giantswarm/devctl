package llm

import (
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

// Preflight checks the repository at root before any LLM output is written.
func Preflight(ctx context.Context, root string, mode Mode) (Report, error) {
	p := &preflight{root: root, mode: mode}
	if err := p.run(ctx); err != nil {
		return Report{}, microerror.Mask(err)
	}
	slices.SortStableFunc(p.report.Findings, func(a, b Finding) int { return strings.Compare(a.Path, b.Path) })
	return p.report, nil
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
}

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
	claudeDirOK, err := p.checkDir(claudeDir)
	if err != nil {
		return microerror.Mask(err)
	}
	agentsDirOK, err := p.checkDir(agentsDir)
	if err != nil {
		return microerror.Mask(err)
	}
	if claudeDirOK {
		if err := p.checkCaseVariants(claudeDir); err != nil {
			return microerror.Mask(err)
		}
		if err := p.checkClaudeSkills(); err != nil {
			return microerror.Mask(err)
		}
	}
	if agentsDirOK {
		if err := p.checkCaseVariants(agentsDir); err != nil {
			return microerror.Mask(err)
		}
		if _, err := p.checkDir(agentsSkillsDir); err != nil {
			return microerror.Mask(err)
		}
	}

	agents, err := p.checkAgentsFile()
	if err != nil {
		return microerror.Mask(err)
	}

	var instructions []string
	if agents != agentsAbsent {
		instructions = append(instructions, agentsFile)
	}
	claudeFiles := []string{claudeFile}
	if claudeDirOK {
		claudeFiles = append(claudeFiles, dotClaudeFile)
	}
	for _, name := range claudeFiles {
		exists, err := p.checkClaudeFile(name)
		if err != nil {
			return microerror.Mask(err)
		}
		if exists {
			instructions = append(instructions, name)
		}
	}

	if err := p.checkIgnored(ctx); err != nil {
		return microerror.Mask(err)
	}

	// Migrating an AGENTS.md that cannot be used would not help.
	needsMigration := (agents == agentsAbsent || agents == agentsWithoutBlock) && len(instructions) > 0
	if p.mode == DefaultMode && needsMigration {
		p.add(instructions[0], "holds instructions that are not in a devctl-managed block yet", migrateFix)
	}
	return nil
}

// checkDir reports a directory that gen llm writes into but that is a link or
// not a directory, and returns whether it is a real directory.
func (p *preflight) checkDir(name string) (bool, error) {
	info, err := p.lstat(name)
	if err != nil || info == nil {
		return false, microerror.Mask(err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		p.add(name, "is a link, so gen llm would change files outside it", "replace the link with a directory")
	case !info.IsDir():
		p.add(name, "is not a directory", "remove or rename it")
	default:
		return true, nil
	}
	return false, nil
}

// checkClaudeSkills checks that .claude/skills is missing or a link to the
// shared skills. A directory is moved there in migration mode, as long as
// every entry is a skill directory without a shared counterpart or a link to
// the shared skill of the same name.
func (p *preflight) checkClaudeSkills() error {
	info, err := p.lstat(claudeSkillsDir)
	if err != nil || info == nil {
		return microerror.Mask(err)
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
		case !info.IsDir():
			p.add(entry, "is a file, not a skill directory", fmt.Sprintf("move it to %s/SKILL.md or remove it", entry[:len(entry)-len(path.Ext(entry))]))
		default:
			exists, err := p.exists(shared)
			if err != nil {
				return microerror.Mask(err)
			}
			if exists {
				p.add(entry, fmt.Sprintf("has the same name as %s", shared), "keep only one of the two skills")
			}
		}
	}
	return nil
}

// checkIgnored reports outputs that Git would ignore. Alignment commits with
// git add -A, which skips them, so it would commit only part of the change.
// Outside a Git checkout there is nothing to check.
func (p *preflight) checkIgnored(ctx context.Context) error {
	inside := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	inside.Dir = p.root
	if err := inside.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) || errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		return microerror.Mask(err)
	}

	// The trailing slash makes Git match the path as a directory.
	outputs := []string{agentsFile, claudeSkillsDir, agentsSkillsDir + "/"}
	cmd := exec.CommandContext(ctx, "git", append([]string{"check-ignore", "--"}, outputs...)...) // #nosec G204 -- fixed arguments, without a shell
	cmd.Dir = p.root
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil
	}
	if err != nil {
		return microerror.Maskf(executionFailedError, "git check-ignore: %s", err)
	}
	for line := range strings.Lines(string(out)) {
		name := strings.TrimSuffix(strings.TrimSpace(line), "/")
		p.add(name, "is ignored by Git, so a commit with git add -A would leave it out", "remove the entry that matches it from .gitignore, as shown by git check-ignore -v "+name)
	}
	return nil
}

func (p *preflight) exists(name string) (bool, error) {
	info, err := p.lstat(name)
	return info != nil, microerror.Mask(err)
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
	hasBlock, problem := file.CheckManagedBlock(content)
	switch {
	case problem != "":
		p.add(agentsFile, problem, file.ManagedBlockFix)
		return agentsUnusable, nil
	case hasBlock:
		return agentsWithBlock, nil
	}
	return agentsWithoutBlock, nil
}

// checkClaudeFile reports a Claude instruction file that is neither a regular
// file nor a link to the root AGENTS.md, and returns whether it exists.
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
	case !info.Mode().IsRegular():
		p.add(name, "is not a regular file", "remove or rename it")
	}
	return true, nil
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
