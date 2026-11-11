package releasewait

import (
	"regexp"
	"strings"
)

// makefileName is the Makefile at the repository root a job's make calls
// read.
const makefileName = "Makefile"

// makefile is what a Makefile declares of its variables and rules: enough to
// follow a make call of a CI job to the commands its target runs.
type makefile struct {
	// vars are the variables the Makefile assigns, unexpanded.
	vars map[string]string
	// prereqs and recipes are each target's prerequisites and recipe lines,
	// over every rule that names it.
	prereqs map[string][]string
	recipes map[string][]string
}

var (
	makeAssignment = regexp.MustCompile(`^(?:override\s+|export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(\?=|:=|::=|\+=|=)\s*(.*)$`)
	makeReference  = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_]*)\)|\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	makeCall       = regexp.MustCompile(`(?:^|[\s;&|(])make\s+([^;&|)\n]*)`)
	shellAssign    = regexp.MustCompile(`(?m)^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
)

// parseMakefile reads the variables and rules of a Makefile; what it does
// not understand (conditionals, functions, pattern rules) is skipped.
func parseMakefile(data []byte) *makefile {
	m := &makefile{vars: map[string]string{}, prereqs: map[string][]string{}, recipes: map[string][]string{}}
	var targets []string
	lines := strings.Split(strings.ReplaceAll(string(data), "\\\n", " "), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "\t") {
			recipe := strings.TrimLeft(strings.TrimSpace(line), "@-+")
			for _, t := range targets {
				m.recipes[t] = append(m.recipes[t], recipe)
			}
			continue
		}
		code, _, _ := strings.Cut(line, "#")
		if strings.TrimSpace(code) == "" {
			continue
		}
		targets = nil
		if a := makeAssignment.FindStringSubmatch(code); a != nil {
			name, op, value := a[1], a[2], strings.TrimSpace(a[3])
			switch _, set := m.vars[name]; {
			case op == "?=" && set:
			case op == "+=" && set:
				m.vars[name] += " " + value
			default:
				m.vars[name] = value
			}
			continue
		}
		if names, rest, ok := strings.Cut(code, ":"); ok && !strings.HasPrefix(code, " ") {
			targets = strings.Fields(names)
			for _, t := range targets {
				m.prereqs[t] = append(m.prereqs[t], strings.Fields(strings.TrimLeft(rest, ":"))...)
			}
		}
	}
	return m
}

// pushTargets are the helm push commands the make call of target with the
// command-line variables vars runs, through its prerequisites, with the
// Makefile's variables expanded; a variable it cannot resolve stays in place.
func (m *makefile) pushTargets(target string, vars map[string]string) (raws []string, commands []string) {
	seen := map[string]bool{}
	var visit func(t string)
	visit = func(t string) {
		if seen[t] {
			return
		}
		seen[t] = true
		for _, p := range m.prereqs[t] {
			visit(p)
		}
		for _, line := range m.recipes[t] {
			line = m.expand(line, vars, 0)
			commands = append(commands, line)
			if match := makeHelmPush.FindStringSubmatch(line); match != nil {
				raws = append(raws, strings.TrimSuffix(strings.TrimPrefix(match[1], "oci://"), "/"))
			}
		}
	}
	visit(target)
	return raws, commands
}

// makeHelmPush is a helm push in a recipe: its target is a make variable as
// often as a literal oci:// reference.
var makeHelmPush = regexp.MustCompile(`helm\s+push\s+\S+\s+["']?([^"'\s]+)`)

// expand replaces the make variables a line references, the command line's
// over the Makefile's, recursively; an unknown one, or one whose value
// make alone computes, stays as written.
func (m *makefile) expand(line string, vars map[string]string, depth int) string {
	if depth > 10 {
		return line
	}
	return makeReference.ReplaceAllStringFunc(line, func(ref string) string {
		r := makeReference.FindStringSubmatch(ref)
		name := r[1] + r[2]
		if value, ok := vars[name]; ok {
			return value
		}
		if value, ok := m.vars[name]; ok {
			// A value of make functions ($(shell …)) is known only to make.
			if value = m.expand(value, vars, depth+1); !strings.Contains(value, "$") {
				return value
			}
		}
		return ref
	})
}

// makeInvocation is a make call in a job's commands: the targets and the
// variables its command line sets.
type makeInvocation struct {
	targets []string
	vars    map[string]string
}

// makeInvocations are the make calls of a job's commands that read the
// root Makefile; a call naming another Makefile or directory is skipped.
// A command-line variable is read as the shell passes it: unquoted and with
// the job's variables expanded, empty when one cannot be resolved.
func makeInvocations(commands string, env map[string]string) []makeInvocation {
	var calls []makeInvocation
	for _, m := range makeCall.FindAllStringSubmatch(commands, -1) {
		call := makeInvocation{vars: map[string]string{}}
		other := false
		for _, word := range shellWords(m[1]) {
			switch {
			case word == "-C" || word == "-f" || strings.HasPrefix(word, "--directory") || strings.HasPrefix(word, "--file") || strings.HasPrefix(word, "--makefile"):
				other = true
			case strings.HasPrefix(word, "-"):
			case makeAssignment.MatchString(word) && strings.Contains(word, "="):
				name, value, _ := strings.Cut(word, "=")
				call.vars[name] = expandShell(value, env)
			default:
				call.targets = append(call.targets, word)
			}
		}
		if !other && len(call.targets) > 0 {
			calls = append(calls, call)
		}
	}
	return calls
}

// shellWords splits a command line into words the way the shell does for
// quotes: a quoted part loses its quotes and keeps its spaces.
func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	in := false
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '"' || r == '\'':
			quote, in = r, true
		case r == ' ' || r == '\t':
			if in {
				words = append(words, word.String())
				word.Reset()
				in = false
			}
		default:
			word.WriteRune(r)
			in = true
		}
	}
	if in {
		words = append(words, word.String())
	}
	return words
}

// expandShell expands the shell variables of a value with env; one env does
// not name leaves the value empty, unknown.
func expandShell(value string, env map[string]string) string {
	resolved := true
	out := shellVariable.ReplaceAllStringFunc(value, func(v string) string {
		m := shellVariable.FindStringSubmatch(v)
		value, ok := env[m[1]+m[2]]
		if !ok {
			resolved = false
		}
		return value
	})
	if !resolved || strings.ContainsAny(out, "$`") {
		return ""
	}
	return out
}

// jobEnvironment is a job's environment with the plain assignments of its
// commands: a variable its commands assign a literal value once is known
// with it, one they assign a computed value (or several) is unknown.
func jobEnvironment(environment map[string]string, commands string) map[string]string {
	env := map[string]string{}
	for k, v := range environment {
		env[k] = v
	}
	assigned, computed := map[string]string{}, map[string]bool{}
	for _, a := range shellAssign.FindAllStringSubmatch(commands, -1) {
		name, value := a[1], strings.TrimSpace(a[2])
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if prev, seen := assigned[name]; strings.ContainsAny(value, "$` \t;&|") || (seen && prev != value) {
			computed[name] = true
		}
		assigned[name] = value
	}
	for name, value := range assigned {
		if computed[name] {
			delete(env, name)
		} else {
			env[name] = value
		}
	}
	return env
}
