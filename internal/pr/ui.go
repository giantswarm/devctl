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
// The columnHeader parameter controls the label of the second column
// (e.g. "Repository" or "Dependency").
func PrintTableHeader(w io.Writer, columnHeader string) {
	header := fmt.Sprintf("%-7s %-40s %-30s", "PR", columnHeader, "Status")
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, strings.Repeat("─", 80))
}

// UpdateTable redraws the PR status table.
// Each PRStatus.DisplayLabel is shown in the second column.
func UpdateTable(w io.Writer, prStatuses []*PRStatus) {
	if len(prStatuses) > 0 {
		fmt.Fprintf(w, "\033[%dA", len(prStatuses))
	}

	for _, ps := range prStatuses {
		prText := fmt.Sprintf("#%-5d", ps.Number)
		prLink := MakeHyperlink(ps.URL, prText)
		status := ps.GetStatus()

		line := fmt.Sprintf("%s  %-40s %-30s", prLink, ps.DisplayLabel, status)
		fmt.Fprintf(w, "\033[2K%s\n", line)
	}
}
