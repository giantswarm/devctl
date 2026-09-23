package authstore

import (
	"fmt"
	"io"
	"sync"
)

// warned is every Warning [Token.WarnOnce] wrote in this process.
var warned sync.Map

// WarnOnce writes the token's Warning to w as one line, once per process: the
// version check that precedes a command and the command itself resolve the
// same token, and its override is told once. No Warning writes nothing.
func (t Token) WarnOnce(w io.Writer) {
	if t.Warning == "" {
		return
	}
	if _, told := warned.LoadOrStore(t.Warning, struct{}{}); told {
		return
	}
	_, _ = fmt.Fprintf(w, "warning: %s\n", t.Warning)
}
