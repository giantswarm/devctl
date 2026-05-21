package release

import "testing"

func TestIsDevVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"3.9.2", false},
		{"v3.9.2", false},
		{"7.3.0-a3f1e2b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0", true},  // full 40-char SHA
		{"v7.3.0-a3f1e2b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0", true}, // with v prefix
		{"7.3.0-abc1234f", false},                                 // short SHA, no longer matches
		{"1.0.0-alpha.1", false},                                  // semver pre-release, not a git SHA
		{"", false},
		{"not-a-version", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := isDevVersion(tt.version)
			if got != tt.want {
				t.Errorf("isDevVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestToTestCatalog(t *testing.T) {
	tests := []struct {
		catalog string
		want    string
	}{
		{"default", "default-test"},
		{"cluster", "cluster-test"},
		{"giantswarm", "giantswarm-test"},
		{"default-test", "default-test"},
		{"cluster-test", "cluster-test"},
	}

	for _, tt := range tests {
		t.Run(tt.catalog, func(t *testing.T) {
			got := toTestCatalog(tt.catalog)
			if got != tt.want {
				t.Errorf("toTestCatalog(%q) = %q, want %q", tt.catalog, got, tt.want)
			}
		})
	}
}
