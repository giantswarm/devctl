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
	if _, err := fmt.Fprintf(w, "%s: %s in %s\n\n", head, state, r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond)); err != nil {
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
		if _, err := fmt.Fprintf(w, "- [%s] %s\n  fix: %s\n", f.Kind, f.Message, f.Fix); err != nil {
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
// many findings it reported (the findings themselves follow the table).
func detail(s StepResult) string {
	var parts []string
	if s.Summary != "" {
		parts = append(parts, s.Summary)
	}
	if len(s.Changes) > 0 {
		parts = append(parts, strings.Join(s.Changes, "; "))
	}
	switch n := len(s.Findings); n {
	case 0:
	case 1:
		parts = append(parts, "1 finding")
	default:
		parts = append(parts, fmt.Sprintf("%d findings", n))
	}
	return strings.Join(parts, " | ")
}
