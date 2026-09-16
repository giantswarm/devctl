package reconcile

import (
	"fmt"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// genCIGenerateField is the declaration field the CircleCI generator's
// refusal names.
const genCIGenerateField = "gen.ci.generate"

// Refused is the result of an entry the validator refused: no step ran, the
// declaration is at fault, and the callers parse the refusal like any other
// finding instead of an exit without a result. The result carries one step,
// [StepEntry], [VerdictReported], with one finding per problem naming the
// field to fix — [FindingGenCircleCIRefused] for gen.ci.generate (the
// CircleCI generator would produce no job), [FindingEntryRefused] for the
// rest. Nothing failed and nothing drifted, so the result is converged.
func Refused(req Request, now time.Time) *Result {
	owner := req.Owner
	if owner == "" {
		owner = reposetup.DefaultOwner
	}
	mode := req.Mode
	if mode == "" {
		mode = ModeCheck
	}
	sr := StepResult{
		Step:    StepEntry,
		Verdict: VerdictReported,
		Summary: fmt.Sprintf("refused: %d problem(s) with the entry in repositories/%s.yaml", len(req.Entry.Problems), req.Team),
	}
	for _, p := range req.Entry.Problems {
		kind := FindingEntryRefused
		if p.Field == genCIGenerateField {
			kind = FindingGenCircleCIRefused
		}
		sr.Findings = append(sr.Findings, Finding{
			Kind:    kind,
			Message: p.String(),
			Fix:     fmt.Sprintf("edit %s of the entry %q in repositories/%s.yaml: %s", p.Field, req.Entry.Name, req.Team, p.Message),
		})
	}
	slug := owner + "/" + req.Entry.Name
	return &Result{
		Repository: slug,
		Declared:   slug,
		Team:       req.Team,
		Mode:       mode,
		Added:      req.Added,
		StartedAt:  now,
		FinishedAt: now,
		Steps:      []StepResult{sr},
		Converged:  true,
	}
}
