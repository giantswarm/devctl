package file

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/titanous/json5"
)

var conversionFailedError = &microerror.Error{
	Kind: "conversionFailedError",
}

// json5Key is a bare JSON5 object key at the start of a line, e.g. `extends:`.
var json5Key = regexp.MustCompile(`^(\s*)([A-Za-z_$][A-Za-z0-9_$]*):`)

// toJSONC converts the JSON5 the template renders into JSONC, the format
// Renovate reads renovate.json in: whole-line comments stay, bare keys are
// quoted, single-quoted strings become double-quoted ones and the trailing
// comma before a closing bracket goes. It handles the template's own output,
// not JSON5 at large, and proves every conversion: the result has to parse
// as JSON, comments aside, to the very value the JSON5 parses to.
func toJSONC(src []byte) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if isComment(line) {
			continue
		}
		line = json5Key.ReplaceAllString(line, `$1"$2":`)
		lines[i] = doubleQuote(line)
	}

	// A comma is trailing when the next line that is no comment closes the
	// array or object.
	last := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isComment(line) {
			continue
		}
		if last >= 0 && (trimmed[0] == ']' || trimmed[0] == '}') {
			lines[last] = strings.TrimSuffix(lines[last], ",")
		}
		last = i
	}

	out := []byte(strings.Join(lines, "\n"))

	var want, got interface{}
	if err := json5.Unmarshal(src, &want); err != nil {
		return nil, microerror.Maskf(conversionFailedError, "the rendered JSON5 does not parse: %v", err)
	}
	if err := json.Unmarshal(stripComments(out), &got); err != nil {
		return nil, microerror.Maskf(conversionFailedError, "the converted JSONC does not parse as JSON: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		return nil, microerror.Maskf(conversionFailedError, "the converted JSONC holds another value than the rendered JSON5")
	}

	return out, nil
}

// doubleQuote rewrites the single-quoted string literals of a line as
// double-quoted ones: an escaped single quote loses its backslash, a double
// quote gains one, every other escape is valid JSON as it is.
func doubleQuote(line string) string {
	var b strings.Builder
	inString := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case !inString && c == '\'':
			inString = true
			b.WriteByte('"')
		case inString && c == '\'':
			inString = false
			b.WriteByte('"')
		case inString && c == '\\' && i+1 < len(line):
			i++
			if line[i] != '\'' {
				b.WriteByte('\\')
			}
			b.WriteByte(line[i])
		case inString && c == '"':
			b.WriteString(`\"`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isComment(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "//")
}

// stripComments drops the whole-line comments, the only ones the template
// writes, leaving JSON.
func stripComments(src []byte) []byte {
	var b bytes.Buffer
	for _, line := range bytes.Split(src, []byte("\n")) {
		if !isComment(string(line)) {
			b.Write(line)
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}
