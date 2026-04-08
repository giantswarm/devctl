package params

import "testing"

func Test_HasFlavor(t *testing.T) {
	testCases := []struct {
		name     string
		flavors  []string
		flavor   string
		expected bool
	}{
		{
			name:     "case 1: empty flavors",
			flavors:  []string{},
			flavor:   "bash",
			expected: false,
		},
		{
			name:     "case 2: flavor present",
			flavors:  []string{"bash", "md"},
			flavor:   "bash",
			expected: true,
		},
		{
			name:     "case 3: flavor absent",
			flavors:  []string{"bash", "md"},
			flavor:   "helmchart",
			expected: false,
		},
		{
			name:     "case 4: single matching flavor",
			flavors:  []string{"helmchart"},
			flavor:   "helmchart",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := Params{Flavors: tc.flavors}
			got := HasFlavor(p, tc.flavor)
			if got != tc.expected {
				t.Errorf("HasFlavor(%v, %q) = %v, want %v", tc.flavors, tc.flavor, got, tc.expected)
			}
		})
	}
}
