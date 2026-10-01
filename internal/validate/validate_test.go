package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Name(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "simple", value: "prometheus", valid: true},
		{name: "hyphenated", value: "cluster-api-provider-aws", valid: true},
		{name: "digits", value: "coredns2", valid: true},
		{name: "dotted", value: "cert-manager.io", valid: true},
		{name: "empty", value: "", valid: false},
		{name: "parent directory", value: "..", valid: false},
		{name: "path traversal", value: "../../etc/passwd", valid: false},
		{name: "absolute path", value: "/etc/passwd", valid: false},
		{name: "path separator", value: "team/other", valid: false},
		{name: "leading dash reads as a flag", value: "-rf", valid: false},
		{name: "long flag", value: "--kubeconfig", valid: false},
		{name: "trailing dash", value: "atlas-", valid: false},
		{name: "uppercase", value: "Atlas", valid: false},
		{name: "space", value: "atlas team", valid: false},
		{name: "semicolon", value: "atlas;id", valid: false},
		{name: "newline", value: "atlas\nid", valid: false},
		{name: "url scheme", value: "http://example.com", valid: false},
		{name: "percent encoding", value: "atlas%2f", valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Name("--team", tc.value)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "--team")
		})
	}
}

func Test_Version(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "semver", value: "1.2.3", valid: true},
		{name: "leading v", value: "v1.2.3", valid: true},
		{name: "prerelease", value: "v1.2.3-rc1", valid: true},
		{name: "build metadata", value: "1.2.3+build.5", valid: true},
		{name: "major only", value: "v2", valid: true},
		{name: "empty", value: "", valid: false},
		{name: "path traversal", value: "../../etc/passwd", valid: false},
		{name: "path separator", value: "1.2.3/..", valid: false},
		{name: "leading dash reads as a flag", value: "-1.2.3", valid: false},
		{name: "query string", value: "1.2.3?x=1", valid: false},
		{name: "space", value: "1.2.3 4", valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Version("--app-version", tc.value)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "--app-version")
		})
	}
}
