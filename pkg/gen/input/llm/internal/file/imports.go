package file

import (
	"bytes"
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	// importToken matches a Claude import: @ and a path, at the start of the
	// text or after whitespace, so that e-mail addresses are not imports.
	importToken = regexp.MustCompile(`(^|\s)@(\S+)`)
	// importTokenAfterCode is importToken in text that follows inline code,
	// where the start of the text is not the start of a line.
	importTokenAfterCode = regexp.MustCompile(`(\s)@(\S+)`)
	// inlineLink matches the target of an inline Markdown link.
	inlineLink = regexp.MustCompile(`\]\(([^)\s]+)`)
	// linkDefinition matches the target of a link reference definition.
	linkDefinition = regexp.MustCompile(`^(\s{0,3}\[[^\]]+\]:\s*)(\S+)`)
)

// trailingPunctuation ends a sentence rather than a path.
const trailingPunctuation = ".,;:!?)'\""

// mapProse calls replace with each piece of content outside code fences and
// inline code, together with whether the piece starts a line, and puts the
// result in its place.
func mapProse(content []byte, replace func(prose []byte, lineStart bool) []byte) []byte {
	var out []byte
	var fence []byte
	for line := range bytes.Lines(content) {
		if fence != nil {
			if closesFence(line, fence) {
				fence = nil
			}
			out = append(out, line...)
			continue
		}
		if fence = opensFence(line); fence != nil {
			out = append(out, line...)
			continue
		}
		// Even segments are outside inline code.
		segments := bytes.Split(line, []byte("`"))
		for i := 0; i < len(segments); i += 2 {
			segments[i] = replace(segments[i], i == 0)
		}
		out = append(out, bytes.Join(segments, []byte("`"))...)
	}
	return out
}

// mapImports calls replace with the path of every import in content, without
// trailing punctuation, and puts the returned text in place of the @ and the
// path.
func mapImports(content []byte, replace func(importPath string) string) []byte {
	return mapProse(content, func(prose []byte, lineStart bool) []byte {
		token := importTokenAfterCode
		if lineStart {
			token = importToken
		}
		return token.ReplaceAllFunc(prose, func(m []byte) []byte {
			sub := token.FindSubmatch(m)
			p := strings.TrimRight(string(sub[2]), trailingPunctuation)
			suffix := string(sub[2])[len(p):]
			if p == "" {
				return m
			}
			return []byte(string(sub[1]) + replace(p) + suffix)
		})
	})
}

func hasImport(content []byte, importPath string) bool {
	found := false
	mapImports(content, func(p string) string {
		if path.Clean(p) == path.Clean(importPath) {
			found = true
		}
		return "@" + p
	})
	return found
}

// AddImport returns content with an import of importPath on its first line,
// unless content imports it already.
func AddImport(content []byte, importPath string) []byte {
	if hasImport(content, importPath) {
		return content
	}
	rest, hasBOM := bytes.CutPrefix(content, utf8BOM)
	eol := lineEnding(rest)
	var out []byte
	if hasBOM {
		out = append(out, utf8BOM...)
	}
	out = append(out, "@"+importPath+eol+eol...)
	return append(out, rest...)
}

// MoveInstructions returns content moved from dir to the root AGENTS.md.
// Relative imports and links that resolve to an existing path from dir are
// rewritten to resolve to it from the root; other text after an @, such as a
// team mention, is kept. Imports of instructionFiles are removed, because from
// AGENTS.md they would import AGENTS.md again: a line with only the import is
// dropped, and an import in a sentence becomes a mention of the path.
func MoveInstructions(content []byte, dir string, exists func(rel string) bool, instructionFiles []string) []byte {
	content, hasBOM := bytes.CutPrefix(content, utf8BOM)

	relative := func(p string) bool {
		return !path.IsAbs(p) && !strings.HasPrefix(p, "~") && !strings.Contains(p, ":")
	}
	isInstruction := func(p string) bool {
		return relative(p) && slices.Contains(instructionFiles, path.Join(dir, p))
	}
	rebase := func(p string) string {
		if !relative(p) {
			return p
		}
		rebased := path.Join(dir, p)
		if rebased == ".." || strings.HasPrefix(rebased, "../") || !exists(rebased) {
			return p
		}
		return rebased
	}

	var kept []byte
	var fence []byte
	removed := false
	for line := range bytes.Lines(content) {
		if fence != nil {
			if closesFence(line, fence) {
				fence = nil
			}
			kept = append(kept, line...)
			continue
		}
		if fence = opensFence(line); fence != nil {
			kept = append(kept, line...)
			continue
		}
		token, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("@"))
		if ok && len(token) > 0 && !bytes.ContainsAny(token, " \t") &&
			isInstruction(strings.TrimRight(string(token), trailingPunctuation)) {
			removed = true
			continue
		}
		kept = append(kept, line...)
	}
	if removed {
		kept = bytes.TrimLeft(kept, "\r\n")
	}

	moved := mapImports(kept, func(p string) string {
		if isInstruction(p) {
			return p
		}
		return "@" + rebase(p)
	})
	moved = mapProse(moved, func(prose []byte, lineStart bool) []byte {
		prose = inlineLink.ReplaceAllFunc(prose, func(m []byte) []byte {
			target := string(m[len("]("):])
			p, fragment, hasFragment := strings.Cut(target, "#")
			if p == "" {
				return m
			}
			rebased := rebase(p)
			if hasFragment {
				rebased += "#" + fragment
			}
			return []byte("](" + rebased)
		})
		if !lineStart {
			return prose
		}
		return linkDefinition.ReplaceAllFunc(prose, func(m []byte) []byte {
			sub := linkDefinition.FindSubmatch(m)
			return []byte(string(sub[1]) + rebase(string(sub[2])))
		})
	})

	if hasBOM {
		return append(append([]byte{}, utf8BOM...), moved...)
	}
	return moved
}
