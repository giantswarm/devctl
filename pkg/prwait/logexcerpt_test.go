package prwait

import "testing"

func TestLogHeadTail(t *testing.T) {
	testCases := []struct {
		name     string
		output   string
		lines    int
		wantHead string
		wantTail string
	}{
		{
			name: "case 0: first and last lines with a gap", output: "first\nsecond\nmiddle\npenultimate\nlast\n", lines: 2,
			wantHead: "first\nsecond", wantTail: "penultimate\nlast",
		},
		{
			name: "case 1: short log without final newline", output: "first\nlast", lines: 5,
			wantHead: "first\nlast", wantTail: "first\nlast",
		},
		{
			name: "case 2: controls and CRLF", output: "\x1b[31mfirst\x1b[0m\r\n\x1b]0;hidden\a\tlast\x00\u202e\n", lines: 1,
			wantHead: "first", wantTail: "\tlast",
		},
		{
			name: "case 3: blank lines count", output: "first\n\nlast\n\n", lines: 2,
			wantHead: "first\n", wantTail: "last\n",
		},
		{name: "case 4: empty output", lines: 2},
		{name: "case 5: zero lines", output: "first\nlast\n"},
		{name: "case 6: negative lines", output: "first\nlast\n", lines: -1},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			head, tail := LogHeadTail(tc.output, tc.lines)
			if head != tc.wantHead || tail != tc.wantTail {
				t.Errorf("head=%q, tail=%q; want head=%q, tail=%q", head, tail, tc.wantHead, tc.wantTail)
			}
		})
	}
}
