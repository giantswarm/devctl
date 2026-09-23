package authstore

import (
	"bytes"
	"testing"
)

func TestTokenWarnOnce(t *testing.T) {
	var w bytes.Buffer
	Token{}.WarnOnce(&w)
	if w.Len() != 0 {
		t.Fatalf("a token without a Warning wrote %q", w.String())
	}

	token := Token{Warning: "the GitHub token in $WARN_ONCE_TEST overrides the devctl GitHub App login."}
	token.WarnOnce(&w)
	token.WarnOnce(&w)
	Token{Warning: token.Warning}.WarnOnce(&w)
	want := "warning: " + token.Warning + "\n"
	if w.String() != want {
		t.Errorf("wrote %q, want it once: %q", w.String(), want)
	}
}
