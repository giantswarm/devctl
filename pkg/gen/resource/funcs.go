package resource

import (
	"strings"
	"unicode"
)

func firstLetterToLower(s string) string {
	if len(s) < 2 {
		return strings.ToLower(s)
	}

	rs := []rune(s)
	rs[0] = unicode.ToLower(rs[0])

	return string(rs)
}
