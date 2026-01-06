package pr

import (
	"fmt"
	"io"
	"strings"
)

// MakeHyperlink creates an ANSI hyperlink (OSC 8) for terminals that support it.
func MakeHyperlink(url, text string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}

// PrintTableHeader prints the header for the PR status table.
func PrintTableHeader(w io.Writer) {
	header := fmt.Sprintf("%-7s %-40s %-30s", "PR", "Repository", "Status")
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, strings.Repeat("─", 80))
}

// UpdateTable redraws the PR status table.
func UpdateTable(w io.Writer, prStatuses []*PRStatus) {
	// Move cursor up to redraw table
	if len(prStatuses) > 0 {
		fmt.Fprintf(w, "\033[%dA", len(prStatuses))
	}

	for _, ps := range prStatuses {
		// Pad PR number to consistent width (6 chars for "#12345")
		prText := fmt.Sprintf("#%-5d", ps.Number)
		prLink := MakeHyperlink(ps.URL, prText)
		status := ps.GetStatus()

		// Don't use padding in format string for hyperlink, just add spaces after
		line := fmt.Sprintf("%s  %-40s %-30s", prLink, ps.Repo, status)
		// Clear line and print
		fmt.Fprintf(w, "\033[2K%s\n", line)
	}
}
