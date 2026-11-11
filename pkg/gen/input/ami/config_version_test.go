package ami

import "testing"

func TestNewValidatesMinimumVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		valid   bool
	}{
		{version: "not-a-version"},
		{version: "1.2.3.4"},
		{version: "18446744073709551616.0.0"},
		{version: "1.2.3", valid: true},
		{version: "v1.2.3", valid: true},
		{version: "1.2", valid: true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			config := Config{
				Arch: "amd64-usr", Channel: "stable", PrimaryDomain: "fixture.invalid",
				ChinaBucketName: "fixture", ChinaBucketRegion: "cn-north-1",
				ChinaAWSAccessKeyID: "fixture", ChinaAWSSecretAccessKey: "fixture", // #nosec G101 -- Synthetic configuration only; this test never makes a request.
				Dir: t.TempDir(), MinimumVersion: tc.version,
			}
			got, err := New(config)
			if tc.valid {
				if err != nil || got == nil {
					t.Fatalf("New rejected supported version %q: %v", tc.version, err)
				}
			} else if got != nil || !IsInvalidConfig(err) {
				t.Fatalf("New accepted invalid version %q: got %v, error %v; want nil, invalid config", tc.version, got != nil, err)
			}
		})
	}
}
