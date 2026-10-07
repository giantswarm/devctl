package reposetup

import "testing"

func TestCreationVisibility(t *testing.T) {
	for declared, want := range map[string]string{"": VisibilityPrivate, VisibilityPrivate: VisibilityPrivate, VisibilityPublic: VisibilityPublic} {
		if got := CreationVisibility(declared); got != want {
			t.Errorf("CreationVisibility(%q) = %q, want %q", declared, got, want)
		}
	}
}

// TestIsPrivate: only public is public; a value outside the enum fails closed.
func TestIsPrivate(t *testing.T) {
	for declared, want := range map[string]bool{VisibilityPrivate: true, VisibilityPublic: false, "internal": true, "Public": true} {
		if got := IsPrivate(declared); got != want {
			t.Errorf("IsPrivate(%q) = %v, want %v", declared, got, want)
		}
	}
}
