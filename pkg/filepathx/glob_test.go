package filepathx

import (
	"strconv"
	"testing"
)

func Test_Glob_MatchString(t *testing.T) {
	testCases := []struct {
		name         string
		inputPattern string
		inputString  string
		epectedMatch bool
		errorMatcher func(err error) bool
	}{
		{
			name:          "case 0: exact match",
			inputPattern:  "exact/match",
			inputString:   "exact/match",
			expectedMatch: true,
			errorMatcher:  nil,
		},
		{
			name:          "case 1: mismatch",
			inputPattern:  "abc",
			inputString:   "xyz",
			expectedMatch: false,
			errorMatcher:  nil,
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Log(tc.name)

			switch {
			case err == nil && tc.errorMatcher == nil:
				// correct; carry on
			case err != nil && tc.errorMatcher == nil:
				t.Fatalf("error == %#v, want nil", err)
			case err == nil && tc.errorMatcher != nil:
				t.Fatalf("error == nil, want non-nil")
			case !tc.errorMatcher(err):
				t.Fatalf("error == %#v, want matching", err)
			}

			if tc.errorMatcher != nil {
				return
			}
		})
	}
}
