package params

import (
	"strconv"
	"testing"
)

func Test_toSnakeCase(t *testing.T) {
	testCases := []struct {
		name           string
		inputString    string
		expectedString string
	}{
		{
			name:           "case 0: starting with capital",
			inputString:    "ExampleOne",
			expectedString: "example_one",
		},
		{
			name:           "case 1: starting with lower",
			inputString:    "exampleTwo",
			expectedString: "example_two",
		},
		{
			name:           "case 3: having capital next to each other",
			inputString:    "MyURL",
			expectedString: "my_url",
		},
		{
			name:           "case 4: having numbers",
			inputString:    "w8ForURL",
			expectedString: "w8_for_url",
		},
		{
			name:           "case 5: having numbers and capital next to each other",
			inputString:    "Gr8URL",
			expectedString: "gr8_url",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Log(tc.name)

			s := toSnakeCase(tc.inputString)

			if s != tc.expectedString {
				t.Fatalf("s = %v, want %v", s, tc.expectedString)
			}
		})
	}
}
