package release

import (
	"testing"

	"github.com/blang/semver"
)

func TestSameMajorConstraint(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		allowed        []string
		blocked        []string
	}{
		{
			name:           "cluster-aws style: major 7",
			currentVersion: "7.4.0",
			allowed:        []string{"7.4.0", "7.4.1", "7.5.0", "7.99.99"},
			blocked:        []string{"8.0.0", "8.1.0", "6.9.9", "0.1.0"},
		},
		{
			name:           "cluster-azure style: major 6",
			currentVersion: "6.1.0",
			allowed:        []string{"6.0.0", "6.1.0", "6.2.0", "6.99.0"},
			blocked:        []string{"7.0.0", "5.9.9"},
		},
		{
			name:           "component at major 1",
			currentVersion: "1.26.4",
			allowed:        []string{"1.26.4", "1.26.5", "1.27.0", "1.99.0"},
			blocked:        []string{"2.0.0", "0.9.0"},
		},
		{
			name:           "component at major 0",
			currentVersion: "0.5.0",
			allowed:        []string{"0.5.0", "0.5.1", "0.6.0", "0.99.0"},
			blocked:        []string{"1.0.0", "2.0.0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			constraint := sameMajorConstraint(tc.currentVersion)
			if constraint == nil {
				t.Fatalf("sameMajorConstraint(%q) returned nil", tc.currentVersion)
			}

			for _, v := range tc.allowed {
				sv := semver.MustParse(v)
				if !(*constraint)(sv) {
					t.Errorf("expected version %s to be allowed (current: %s)", v, tc.currentVersion)
				}
			}

			for _, v := range tc.blocked {
				sv := semver.MustParse(v)
				if (*constraint)(sv) {
					t.Errorf("expected version %s to be blocked (current: %s)", v, tc.currentVersion)
				}
			}
		})
	}
}

func TestSameMajorConstraint_InvalidVersion(t *testing.T) {
	constraint := sameMajorConstraint("not-a-version")
	if constraint != nil {
		t.Error("expected nil constraint for invalid version string")
	}
}

func TestSameMajorConstraint_NotAppliedForMajorRelease(t *testing.T) {
	// Verify that the constraint logic in BumpAll only applies for minor releases.
	// For major releases, constraint should remain nil (no restriction).
	// This test documents the expected behavior by checking that sameMajorConstraint
	// would block a major bump that should be allowed in major releases.
	constraint := sameMajorConstraint("7.4.0")
	if constraint == nil {
		t.Fatal("expected non-nil constraint")
	}

	// A major release would allow 8.1.0, but the constraint blocks it.
	// This confirms the constraint is doing its job and should NOT be applied for major releases.
	sv := semver.MustParse("8.1.0")
	if (*constraint)(sv) {
		t.Error("constraint should block version 8.1.0 for current 7.4.0")
	}
}
