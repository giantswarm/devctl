package xstrings

import (
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_FirstLetterToLower(t *testing.T) {
	testCases := []struct {
		name           string
		inputString    string
		expectedString string
	}{
		{
			name:           "case 0",
			inputString:    "ConfigMap",
			expectedString: "configMap",
		},
		{
			name:           "case 1",
			inputString:    "configMap",
			expectedString: "configMap",
		},
		{
			name:           "case 2",
			inputString:    "CONFIGMAP",
			expectedString: "cONFIGMAP",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s := FirstLetterToLower(tc.inputString)
			if !cmp.Equal(s, tc.expectedString) {
				t.Fatalf("\n\n%s\n", cmp.Diff(s, tc.expectedString))
			}
		})
	}
}
