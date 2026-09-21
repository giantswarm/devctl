package reconcile

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// WriteTable renders the result for a person: a header naming the
// repository (and the declared name after a rename), the mode and whether
// the run converged; one row per step with its verdict and detail; then
// every finding with its fix.
func (r *Result) WriteTable(w io.Writer) error {
	head := fmt.Sprintf("%s (%s)", r.Repository, r.Mode)
	if r.Declared != r.Repository {
		head += " — declared as " + r.Declared
	}
	state := "converged"
	if !r.Converged {
		state = "not converged"
	}
	line := fmt.Sprintf("%s: %s in %s", head, state, r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond))
	if cost := r.Requests.String(); cost != "" {
		line += ", " + cost
	}
	if _, err := fmt.Fprintf(w, "%s\n\n", line); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STEP\tVERDICT\tDETAIL")
	for _, s := range r.Steps {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Step, s.Verdict, detail(s))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	findings := r.Findings()
	if len(findings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\nFindings:"); err != nil {
		return err
	}
	for _, f := range findings {
		kind := string(f.Kind)
		if f.Advisory {
			kind += ", advisory"
		}
		if _, err := fmt.Fprintf(w, "- [%s] %s\n  fix: %s\n", kind, f.Message, f.Fix); err != nil {
			return err
		}
	}
	return nil
}

// Failed returns the steps that could not run to their end.
func (r *Result) Failed() []StepResult {
	var failed []StepResult
	for _, s := range r.Steps {
		if s.Verdict == VerdictFailed {
			failed = append(failed, s)
		}
	}
	return failed
}

// detail is the table cell of a step: its summary, its changes, and how
// many findings it reported and how many of them are advisory (the findings
// themselves follow the table).
func detail(s StepResult) string {
	var parts []string
	if s.Summary != "" {
		parts = append(parts, s.Summary)
	}
	if len(s.Changes) > 0 {
		parts = append(parts, strings.Join(s.Changes, "; "))
	}
	if len(s.Findings) > 0 {
		parts = append(parts, findingsCount(s.Findings))
	}
	return strings.Join(parts, " | ")
}

// findingsCount is the findings part of the cell: how many, and how many of
// them are advisory when not all are.
func findingsCount(findings []Finding) string {
	advisory := 0
	for _, f := range findings {
		if f.Advisory {
			advisory++
		}
	}
	n, noun := len(findings), "findings"
	if n == 1 {
		noun = "finding"
	}
	switch {
	case advisory == n:
		return fmt.Sprintf("%d advisory %s", n, noun)
	case advisory > 0:
		return fmt.Sprintf("%d %s, %d advisory", n, noun, advisory)
	}
	return fmt.Sprintf("%d %s", n, noun)
}
