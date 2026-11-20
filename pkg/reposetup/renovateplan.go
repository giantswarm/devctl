package reposetup

import (
	"slices"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// RenovatePlan is what happens to a repository's Renovate configuration when
// its entry is generated: the scaffold of `devctl repo create` and every
// align-files run take it from [PlanRenovate], so the two cannot drift.
type RenovatePlan struct {
	// Generate runs `devctl gen renovate`: renovate.json5 is written.
	Generate bool `json:"generate"`
	// PatchReviewers runs `devctl gen renovate reviewers` on the
	// configuration the repository keeps: the entry's choreReviewers are set
	// in it.
	PatchReviewers bool `json:"patchReviewers"`
}

// PlanRenovate decides what happens to the Renovate configuration of an
// entry's repository; ownConfig says whether the repository carries one of
// its own (renovate.OwnConfig): a scaffold has none, a template's is not its
// own, and one devctl generated is devctl's.
//
// An entry that declares renovate: false gets neither. An entry with
// gen.flavours that is no fork line gets renovate.json5 generated on
// generated CI (gen.ci.generate), whose architect orb the generated
// configuration leaves to devctl, for the customer flavour, whose generated
// set-up is GitHub workflows and Renovate, and when its repository has no
// configuration of its own, so none starts without one and a generated one
// stays generated: a renovate-custom.json5 added later is extended. Every
// other repository keeps the configuration it has, with the chore reviewers
// patched into it.
func PlanRenovate(f Fields, ownConfig bool) RenovatePlan {
	if !f.RenovateOn() {
		return RenovatePlan{}
	}
	if g := f.Gen; g != nil && len(g.Flavours) > 0 && generates(g.Flavours) {
		generateCI := g.CI != nil && g.CI.Generate != nil && *g.CI.Generate
		if generateCI || slices.Contains(g.Flavours, gen.FlavourCustomer.String()) || !ownConfig {
			return RenovatePlan{Generate: true}
		}
	}
	return RenovatePlan{PatchReviewers: len(f.ChoreReviewers) > 0}
}
