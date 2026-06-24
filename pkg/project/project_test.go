package project

import "testing"

func TestVersionFallback(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		buildInfo string
		gitSHA    string
		want      string
	}{
		{"nothing available", dev, "", "n/a", dev},
		{"explicit version ldflag wins", "8.23.1", "8.0.0", "abc1234", "8.23.1"},
		{"build info supplies version", dev, "8.23.1", "abc1234", "8.23.1"},
		{"build info absent; sha fallback", dev, "", "abc1234", "abc1234"},
		{"build info beats sha", dev, "8.23.1", "abc1234", "8.23.1"},
	}

	origVersion, origSHA, origBuildInfo := version, gitSHA, buildInfoVersion
	t.Cleanup(func() { version, gitSHA, buildInfoVersion = origVersion, origSHA, origBuildInfo })

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version = tc.version
			gitSHA = tc.gitSHA
			buildInfoVersion = func() string { return tc.buildInfo }
			if got := Version(); got != tc.want {
				t.Errorf("Version() = %q, want %q", got, tc.want)
			}
		})
	}
}
