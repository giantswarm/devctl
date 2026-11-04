package prwait

import "strings"

// LogHeadTail returns the first and last n lines of output, without terminal
// control sequences or control characters except tab and line breaks. The
// excerpts overlap for short output. A nonpositive n returns empty excerpts.
func LogHeadTail(output string, n int) (head, tail string) {
	if n <= 0 || output == "" {
		return "", ""
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for i := range lines {
		lines[i] = stripControls(lines[i])
	}
	n = min(n, len(lines))
	return strings.Join(lines[:n], "\n"), strings.Join(lines[len(lines)-n:], "\n")
}
