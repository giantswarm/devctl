package file

import (
	"bytes"
	"path"
	"regexp"
	"strings"
)

// importToken matches a Claude import: @ and a path, at the start of the text
// or after whitespace, so that e-mail addresses are not imports.
var importToken = regexp.MustCompile(`(^|\s)@(\S+)`)

// mapImports calls replace with the path of every import in content outside
// code fences and inline code, and puts the returned path in its place.
func mapImports(content []byte, replace func(importPath string) string) []byte {
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
			segments[i] = importToken.ReplaceAllFunc(segments[i], func(m []byte) []byte {
				sub := importToken.FindSubmatch(m)
				return append(append(sub[1], '@'), replace(string(sub[2]))...)
			})
		}
		out = append(out, bytes.Join(segments, []byte("`"))...)
	}
	return out
}

func hasImport(content []byte, importPath string) bool {
	found := false
	mapImports(content, func(p string) string {
		if path.Clean(p) == path.Clean(importPath) {
			found = true
		}
		return p
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

// RemoveImport returns content without the lines that only import importPath,
// and without the blank lines this leaves at its start.
func RemoveImport(content []byte, importPath string) []byte {
	var out []byte
	removed := false
	for line := range bytes.Lines(content) {
		token, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("@"))
		if ok && len(token) > 0 && path.Clean(string(token)) == path.Clean(importPath) {
			removed = true
			continue
		}
		out = append(out, line...)
	}
	if removed {
		out = bytes.TrimLeft(out, "\r\n")
	}
	return out
}

// RebaseImports returns content moved from dir to the repository root, with
// each relative import that resolves to an existing path from dir rewritten
// to resolve to it from the root. Other text after an @, such as a team
// mention, is kept.
func RebaseImports(content []byte, dir string, exists func(rel string) bool) []byte {
	return mapImports(content, func(p string) string {
		if path.IsAbs(p) || strings.HasPrefix(p, "~") || strings.Contains(p, "://") {
			return p
		}
		rebased := path.Join(dir, p)
		if rebased == ".." || strings.HasPrefix(rebased, "../") || !exists(rebased) {
			return p
		}
		return rebased
	})
}
