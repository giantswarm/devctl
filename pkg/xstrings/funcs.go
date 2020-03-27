package xstrings

import (
	"strings"
	"unicode"
)

func Contains(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}

	return false
}

func FirstLetterToLower(s string) string {
	if len(s) < 2 {
		return strings.ToLower(s)
	}

	rs := []rune(s)
	rs[0] = unicode.ToLower(rs[0])

	return string(rs)
}
