package resource

import (
	"strings"
	"unicode"
)

func containsString(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}

	return false
}

func firstLetterToLower(s string) string {
	if len(s) < 2 {
		return strings.ToLower(s)
	}

	rs := []rune(s)
	rs[0] = unicode.ToLower(rs[0])

	return string(rs)
}
