package reposetup

import (
	"fmt"
	"strings"
)

// Undeclared describes a repository that has no entry in a team file, for
// the callers that set a repository up from flags (`devctl repo setup`,
// `repo checks`, `repo reconcile --team`).
type Undeclared struct {
	// Name of the repository. Required.
	Name string
	// ComponentType of the catalog; left out of the entry when empty.
	ComponentType string
	// Lifecycle is the declared lifecycle (archived, deleted); left out when empty.
	Lifecycle string
}

// UndeclaredEntry returns an accepted entry for a repository without a
// team-file declaration. It carries the name and what u says and nothing
// else: it derives no template, so the scaffold step cannot repair with it
// — the settings, permissions, protection, CircleCI, Renovate, metadata
// and lifecycle steps are what such an entry is for.
func UndeclaredEntry(u Undeclared) Entry {
	var b strings.Builder
	fmt.Fprintf(&b, "- name: %s\n", u.Name)
	if u.ComponentType != "" {
		fmt.Fprintf(&b, "  componentType: %s\n", u.ComponentType)
	}
	if u.Lifecycle != "" {
		fmt.Fprintf(&b, "  lifecycle: %s\n", u.Lifecycle)
	}
	return Entry{
		Name:      u.Name,
		Rendered:  b.String(),
		NameCheck: NameCheck{Verdict: VerdictUnchecked, Detail: "the repository exists; the name is not checked"},
		Accepted:  true,
	}
}
