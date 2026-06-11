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

// detectQuoteStyle inspects the string literals already present in src and
// returns the dominant quote character ('\” or '"'). Configs in the wild mix
// the two styles, so the majority wins; ties and quote-less files fall back to
// the extension-based default.
func detectQuoteStyle(src []byte, defaultSingle bool) byte {
	single, double := countStringQuotes(src)
	switch {
	case single > double:
		return '\''
	case double > single:
		return '"'
	default:
		if defaultSingle {
			return '\''
		}
		return '"'
	}
}

// countStringQuotes counts how many string literals are opened with a single
// vs a double quote. It is comment-aware and skips the interior of each string,
// so quote characters appearing inside another string or a comment (a common
// case in Renovate regex managers) are not counted.
func countStringQuotes(src []byte) (single, double int) {
	i := 0
	for i < len(src) {
		i = skipSpaceAndComments(src, i)
		if i >= len(src) {
			break
		}
		switch src[i] {
		case '\'', '"':
			if src[i] == '\'' {
				single++
			} else {
				double++
			}
			_, next, err := readString(src, i)
			if err != nil {
				return single, double
			}
			i = next
		default:
			i++
		}
	}
	return single, double
}

// rootKeysAreQuoted reports whether the keys of the root object are written as
// quoted strings (e.g. "reviewers":) rather than bare JSON5 identifiers
// (reviewers:). It inspects the first key; an empty object falls back to the
// provided default.
func rootKeysAreQuoted(src []byte, objStart int, fallback bool) bool {
	i := skipSpaceAndComments(src, objStart+1)
	if i >= len(src) || src[i] == '}' {
		return fallback
	}
	return src[i] == '"' || src[i] == '\''
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
