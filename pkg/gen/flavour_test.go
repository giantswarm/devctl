package gen

import (
	"strings"
	"testing"
)

// Test_FlavourDescriptions guards the pairing that let the "customer" flavour go
// undocumented: the flag usage string is generated from AllFlavours() while the
// command long descriptions used to carry a hand-maintained bullet list, so
// adding a flavour silently left the help text behind.
func Test_FlavourDescriptions(t *testing.T) {
	for _, f := range AllFlavours() {
		if flavourDescriptions[Flavour(f)] == "" {
			t.Errorf("flavour %q has no description", f)
		}
	}

	for f := range flavourDescriptions {
		if _, err := NewFlavour(f.String()); err != nil {
			t.Errorf("described flavour %q is not a valid flavour", f)
		}
	}
}

func Test_FlavourDescriptionList(t *testing.T) {
	list := FlavourDescriptionList()

	for _, f := range AllFlavours() {
		if !strings.Contains(list, "  - "+f+" - ") {
			t.Errorf("description list is missing flavour %q:\n%s", f, list)
		}
	}
}
