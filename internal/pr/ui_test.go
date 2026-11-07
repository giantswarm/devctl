package pr

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
)

func TestUpdateTableFitsReservedRows(t *testing.T) {
	testCases := []struct {
		name   string
		number int
		label  string
		status string
	}{
		{
			name:   "case 0: long dependency path",
			number: 42,
			label:  "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2",
			status: "Failed checks",
		},
		{
			name:   "case 1: long production status",
			number: 42,
			label:  "storybook",
			status: "Approved, checking merge status...",
		},
		{
			name:   "case 2: wide characters in dependency group",
			number: 42,
			label:  strings.Repeat("界", 24),
			status: "Merged (squash)",
		},
		{
			name:   "case 3: large repository PR number",
			number: 1234567,
			label:  "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2",
			status: "Failed checks",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ps := &PRStatus{
				Number:       tc.number,
				DisplayLabel: tc.label,
				URL:          "https://github.com/example/fixture/pull/42",
				Status:       tc.status,
			}
			var output bytes.Buffer
			// Callers reserve one blank row per PR, and every redraw moves
			// up by exactly that many rows. Wrapping would invalidate it.
			for redraw := range 2 {
				if redraw == 1 {
					ps.UpdateStatus("Merged (squash)")
				}
				output.Reset()
				UpdateTable(&output, []*PRStatus{ps})
				if !strings.HasPrefix(output.String(), "\x1b[1A") {
					t.Fatal("redraw did not return to the reserved PR row")
				}
				if !strings.Contains(output.String(), "\x1b]8;;"+ps.URL+"\x1b\\#"+strconv.Itoa(ps.Number)) {
					t.Fatal("PR hyperlink was lost")
				}
				// Remove only the renderer's cursor/erase and OSC 8 sequences;
				// the URL itself occupies no terminal cells.
				visible := strings.TrimPrefix(output.String(), "\x1b[1A\x1b[2K")
				visible = strings.ReplaceAll(visible, "\x1b]8;;"+ps.URL+"\x1b\\", "")
				visible = strings.ReplaceAll(visible, "\x1b]8;;\x1b\\", "")
				visible = strings.TrimSuffix(visible, "\n")
				if strings.Contains(visible, "\n") {
					t.Fatalf("PR occupies more than one logical row: %q", visible)
				}
				if width := text.StringWidth(visible); width > 79 {
					t.Errorf("row uses %d columns and wraps in an 80-column terminal: %q", width, visible)
				}
				if !strings.Contains(visible, strings.Fields(ps.GetStatus())[0]) {
					t.Errorf("status disappeared from row: %q", visible)
				}
			}
		})
	}
}
