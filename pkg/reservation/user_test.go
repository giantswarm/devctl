package reservation

import (
	"strings"
	"testing"
)

func TestValidateUser(t *testing.T) {
	tests := []struct {
		user string
		ok   bool
	}{
		{"piontec", true},
		{"Luk-asz1", true},
		{"mc-reservations-gitops[bot]", true},
		{"x[bot]", true},
		{"a", true},
		{strings.Repeat("a", 39), true},
		{strings.Repeat("a", 40), false},
		{"-abc", false},
		{"abc-", false},
		{"a--b", false},
		{`a"b`, false},
		{"a b", false},
		{"a[bot]x", false},
		{"[bot]", false},
		{"", false},
	}
	for _, tc := range tests {
		err := ValidateUser(tc.user)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateUser(%q) = %v, want ok=%v", tc.user, err, tc.ok)
		}
	}
}
