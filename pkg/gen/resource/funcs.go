package resource

import (
	"bytes"
	"strings"
)

func firstLetterToLower(s string) string {
	if len(s) < 2 {
		return strings.ToLower(s)
	}

	bs := []byte(s)

	lc := bytes.ToLower([]byte{bs[0]})
	rest := bs[1:]

	return string(bytes.Join([][]byte{lc, rest}, nil))
}
