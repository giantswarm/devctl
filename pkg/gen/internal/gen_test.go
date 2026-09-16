package internal

import (
	"bytes"
	"context"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

func Test_Execute(t *testing.T) {
	generate := func(string) func(context.Context) ([]byte, error) {
		return func(context.Context) ([]byte, error) {
			return []byte("from Generate"), nil
		}
	}

	testCases := []struct {
		name             string
		in               input.Input
		expected         string
		expectedErrCheck func(error) bool
	}{
		{
			name:     "case 0: TemplateBody only",
			in:       input.Input{TemplateBody: "from {{ . }}"},
			expected: "from TemplateBody",
		},
		{
			name:     "case 1: Generate only",
			in:       input.Input{Generate: generate("")},
			expected: "from Generate",
		},
		{
			name:             "case 2: both set is rejected",
			in:               input.Input{TemplateBody: "from {{ . }}", Generate: generate("")},
			expectedErrCheck: IsInvalidInput,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer

			tc.in.TemplateData = "TemplateBody"
			err := Execute(t.Context(), &buf, tc.in)

			if tc.expectedErrCheck != nil {
				if err == nil {
					t.Fatalf("expected error, got none and output %q", buf.String())
				}
				if !tc.expectedErrCheck(err) {
					t.Fatalf("unexpected error %#v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error %#v", err)
			}
			if buf.String() != tc.expected {
				t.Errorf("got %q, want %q", buf.String(), tc.expected)
			}
		})
	}
}
