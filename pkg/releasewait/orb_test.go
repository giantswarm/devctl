package releasewait

import "testing"

func TestParseArchitectOrbPin(t *testing.T) {
	for name, tc := range map[string]struct{ config, want string }{
		"generated":       {"orbs:\n  architect: giantswarm/architect@10.12.0\n", "10.12.0"},
		"another alias":   {"orbs:\n  gs: giantswarm/architect@10.11.1\n  go: circleci/go@1.0.0\n", "10.11.1"},
		"dev version":     {"orbs:\n  architect: giantswarm/architect@dev:my-branch\n", "dev:my-branch"},
		"inline orb only": {"orbs:\n  local:\n    jobs: {}\n", ""},
		"no orbs":         {"version: 2.1\n", ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseArchitectOrbPin([]byte(tc.config))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("pin: want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestPrereleasesToTestCatalog(t *testing.T) {
	for pin, want := range map[string]bool{
		"10.12.0":       true,
		"10.12.1":       true,
		"11.0.0":        true,
		"dev:my-branch": true,
		"10.11.1":       false,
		"9.6.0":         false,
		"":              false,
		"volatile":      false,
	} {
		if got := PrereleasesToTestCatalog(pin); got != want {
			t.Errorf("PrereleasesToTestCatalog(%q): want %v, got %v", pin, want, got)
		}
	}
}

func TestIsPrerelease(t *testing.T) {
	for version, want := range map[string]bool{
		"1.2.3":         false,
		"1.2.3+build.5": false,
		"1.2.3-rc.1":    true,
		"1.2.3-alpha":   true,
		"not-a-version": false,
	} {
		if got := isPrerelease(version); got != want {
			t.Errorf("isPrerelease(%q): want %v, got %v", version, want, got)
		}
	}
}
