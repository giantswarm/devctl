package renovate

import "github.com/giantswarm/microerror"

// keySpan records the byte offsets of a top-level key/value pair within the
// root object. valueStart is the offset of the first byte of the value and
// valueEnd is the offset just past its last byte (exclusive).
type keySpan struct {
	keyStart   int
	valueStart int
	valueEnd   int
}

// rootStyle captures the formatting conventions of the root object, so an
// inserted `reviewers` entry blends in with what is already there.
type rootStyle struct {
	// valueQuote is the dominant quote character (single or double) used by
	// the file's top-level string values.
	valueQuote byte
	// keyQuoted reports whether root keys are written as quoted strings
	// ("reviewers":) rather than bare JSON5 identifiers (reviewers:).
	keyQuoted bool
	// keyQuote is the quote character used for keys when keyQuoted is true.
	keyQuote byte
	// indent is the leading whitespace shared by the root members.
	indent string
}

// analyzeRootStyle walks the top-level members of the root object once and
// derives the conventions used to render an inserted entry: the dominant quote
// of the top-level values, the key-quoting style taken from the first key, and
// the members' indentation.
//
// Only string literals shallow within each top-level value are counted toward
// the quote vote (a scalar value, or the direct elements of a top-level array
// such as `extends`). Strings buried deeper - notably the double-quoted regex
// in a `customManagers`/`packageRules` block - are ignored so they cannot
// outvote the file's house style.
func analyzeRootStyle(src []byte, objStart int, defaultSingle bool) rootStyle {
	st := rootStyle{indent: "  "}

	var single, double int
	first := true
	i := objStart + 1
	for {
		i = skipSpaceAndComments(src, i)
		if i >= len(src) || src[i] == '}' {
			break
		}

		keyStart := i
		_, afterKey, err := readKey(src, i)
		if err != nil {
			break
		}

		if first {
			if c := src[keyStart]; c == '"' || c == '\'' {
				st.keyQuoted = true
				st.keyQuote = c
			}
			if indent, ok := lineIndent(src, keyStart); ok {
				st.indent = indent
			}
			first = false
		}

		i = skipSpaceAndComments(src, afterKey)
		if i >= len(src) || src[i] != ':' {
			break
		}
		i++

		valueStart := skipSpaceAndComments(src, i)
		valueEnd, err := skipValue(src, valueStart)
		if err != nil {
			break
		}
		addShallowStringQuotes(src, valueStart, valueEnd, &single, &double)

		i = skipSpaceAndComments(src, valueEnd)
		if i < len(src) && src[i] == ',' {
			i++
		}
	}

	switch {
	case single > double:
		st.valueQuote = '\''
	case double > single:
		st.valueQuote = '"'
	default:
		if defaultSingle {
			st.valueQuote = '\''
		} else {
			st.valueQuote = '"'
		}
	}

	// An empty (or quote-less) object reveals no key-quoting convention, so
	// fall back to the strictness implied by the extension: strict JSON quotes
	// its keys, the JSON5 house style does not.
	if first {
		st.keyQuoted = !defaultSingle
		st.keyQuote = st.valueQuote
	}

	return st
}

// addShallowStringQuotes counts the single- vs double-quoted string literals in
// src[start:end] that sit at most one container deep, so the direct elements of
// a top-level array are counted but anything nested further is not.
func addShallowStringQuotes(src []byte, start, end int, single, double *int) {
	depth := 0
	i := start
	for i < end {
		i = skipSpaceAndComments(src, i)
		if i >= end {
			break
		}
		switch c := src[i]; {
		case c == '{' || c == '[':
			depth++
			i++
		case c == '}' || c == ']':
			depth--
			i++
		case c == '\'' || c == '"':
			if depth <= 1 {
				if c == '\'' {
					*single++
				} else {
					*double++
				}
			}
			_, next, err := readString(src, i)
			if err != nil {
				return
			}
			i = next
		default:
			i++
		}
	}
}

// lineIndent returns the run of leading whitespace on pos's line, up to pos. ok
// is false when pos is preceded by non-whitespace on its line (e.g. a
// single-line object), so the caller can keep its default indentation.
func lineIndent(src []byte, pos int) (string, bool) {
	start := pos
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	for j := start; j < pos; j++ {
		if src[j] != ' ' && src[j] != '\t' {
			return "", false
		}
	}
	return string(src[start:pos]), true
}

// findRootObjectStart returns the index of the root object's opening brace,
// skipping any leading whitespace and comments. It errors if the document does
// not start with an object.
func findRootObjectStart(src []byte) (int, error) {
	i := skipSpaceAndComments(src, 0)
	if i >= len(src) || src[i] != '{' {
		return 0, microerror.Maskf(invalidConfigError, "expected the config to start with a JSON object ('{')")
	}
	return i, nil
}

// findTopLevelKey scans the root object that begins at objStart and looks for a
// top-level key equal to want. It returns the located span and found=true on a
// match, found=false if the object closes without the key, or an error if the
// structure is malformed.
//
// The scan is comment- and string-aware and only ever considers keys at the
// root nesting level, so a `reviewers` key nested inside e.g. a packageRule is
// never mistaken for the top-level one.
func findTopLevelKey(src []byte, objStart int, want string) (keySpan, bool, error) {
	i := objStart + 1 // step past '{'

	for {
		i = skipSpaceAndComments(src, i)
		if i >= len(src) {
			return keySpan{}, false, microerror.Maskf(invalidConfigError, "unexpected end of file inside root object")
		}
		if src[i] == '}' {
			// End of the root object, key not present.
			return keySpan{}, false, nil
		}

		keyStart := i
		key, afterKey, err := readKey(src, i)
		if err != nil {
			return keySpan{}, false, microerror.Mask(err)
		}

		i = skipSpaceAndComments(src, afterKey)
		if i >= len(src) || src[i] != ':' {
			return keySpan{}, false, microerror.Maskf(invalidConfigError, "expected ':' after key %#q", key)
		}
		i++ // step past ':'

		valueStart := skipSpaceAndComments(src, i)
		valueEnd, err := skipValue(src, valueStart)
		if err != nil {
			return keySpan{}, false, microerror.Mask(err)
		}

		if key == want {
			return keySpan{keyStart: keyStart, valueStart: valueStart, valueEnd: valueEnd}, true, nil
		}

		i = skipSpaceAndComments(src, valueEnd)
		if i < len(src) && src[i] == ',' {
			i++ // step past the separator and look for the next key
			continue
		}
		// No separator: the next non-space byte must close the object,
		// otherwise the document is malformed. Let the loop re-check.
	}
}

// readKey reads an object key starting at i, which may be a single- or
// double-quoted string or an unquoted JSON5 identifier. It returns the decoded
// key and the offset just past it.
func readKey(src []byte, i int) (string, int, error) {
	if i >= len(src) {
		return "", i, microerror.Maskf(invalidConfigError, "unexpected end of file, expected a key")
	}

	if c := src[i]; c == '"' || c == '\'' {
		return readString(src, i)
	}

	start := i
	for i < len(src) && isIdentChar(src[i]) {
		i++
	}
	if i == start {
		return "", i, microerror.Maskf(invalidConfigError, "expected a key at offset %d", start)
	}

	return string(src[start:i]), i, nil
}

// isIdentChar reports whether c may appear in an unquoted JSON5 identifier key.
func isIdentChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '$':
		return true
	default:
		return false
	}
}

// readString reads a single- or double-quoted string starting at i (which must
// point at the opening quote) and returns the decoded content and the offset
// just past the closing quote. Escape sequences are decoded naively, which is
// sufficient for key comparison.
func readString(src []byte, i int) (string, int, error) {
	quote := src[i]
	i++

	var b []byte
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\\':
			if i+1 < len(src) {
				b = append(b, src[i+1])
				i += 2
			} else {
				i++
			}
		case c == quote:
			return string(b), i + 1, nil
		default:
			b = append(b, c)
			i++
		}
	}

	return "", i, microerror.Maskf(invalidConfigError, "unterminated string literal")
}

// skipValue consumes a single value starting at i (objects, arrays, strings or
// primitives) and returns the offset just past it.
func skipValue(src []byte, i int) (int, error) {
	if i >= len(src) {
		return i, microerror.Maskf(invalidConfigError, "unexpected end of file, expected a value")
	}

	switch c := src[i]; {
	case c == '{' || c == '[':
		return skipBracketed(src, i)
	case c == '"' || c == '\'':
		_, next, err := readString(src, i)
		return next, microerror.Mask(err)
	default:
		// A primitive: number, bool, null or similar. It ends at the next
		// structural delimiter, whitespace or comment.
		start := i
		for i < len(src) {
			c := src[i]
			if c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				break
			}
			if c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
				break
			}
			i++
		}
		if i == start {
			return i, microerror.Maskf(invalidConfigError, "expected a value at offset %d", start)
		}
		return i, nil
	}
}

// skipBracketed consumes a bracketed value (object or array) starting at the
// opening bracket at i and returns the offset just past the matching closing
// bracket. It is string- and comment-aware and counts both brace and bracket
// nesting with a single depth, which is correct for well-formed JSON5.
func skipBracketed(src []byte, i int) (int, error) {
	depth := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*'):
			i = skipSpaceAndComments(src, i)
		case c == '"' || c == '\'':
			_, next, err := readString(src, i)
			if err != nil {
				return i, microerror.Mask(err)
			}
			i = next
		case c == '{' || c == '[':
			depth++
			i++
		case c == '}' || c == ']':
			depth--
			i++
			if depth == 0 {
				return i, nil
			}
		default:
			i++
		}
	}

	return i, microerror.Maskf(invalidConfigError, "unbalanced brackets")
}

// skipSpaceAndComments advances past any whitespace, line comments (//...) and
// block comments (/*...*/) starting at i and returns the offset of the next
// significant byte.
func skipSpaceAndComments(src []byte, i int) int {
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2 // step past the closing */
			if i > len(src) {
				i = len(src)
			}
		default:
			return i
		}
	}
	return i
}
