package params

import (
	"regexp"
	"strings"
)

func FileNameSuffix(p Params) string {
	return toSnakeCase(p.Type) + ".go"
}

var matchFirstCap = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func toSnakeCase(s string) string {
	s = matchFirstCap.ReplaceAllString(s, "${1}_${2}")
	return strings.ToLower(s)
}
