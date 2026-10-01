package precommit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_flag_Validate(t *testing.T) {
	testCases := []struct {
		name  string
		flag  flag
		valid bool
	}{
		{
			name:  "no flavors",
			flag:  flag{Language: "go"},
			valid: true,
		},
		{
			name:  "known flavors",
			flag:  flag{Language: "generic", Flavors: []string{"bash", "md", "helmchart"}},
			valid: true,
		},
		{
			name:  "unknown flavor",
			flag:  flag{Language: "go", Flavors: []string{"rust"}},
			valid: false,
		},
		{
			name:  "go generate on a go repository",
			flag:  flag{Language: "go", GoGenerate: true},
			valid: true,
		},
		// The step is rendered inside the Go section of the workflow template,
		// so asking for it anywhere else silently does nothing.
		{
			name:  "go generate on a generic repository",
			flag:  flag{Language: "generic", GoGenerate: true},
			valid: false,
		},
		{
			name:  "go generate without a language",
			flag:  flag{GoGenerate: true},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.flag.Validate()
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}
