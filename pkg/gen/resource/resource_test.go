package resource

import (
	"bytes"
	"context"
	"flag"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"testing"
	"unicode"

	"github.com/giantswarm/devctl/pkg/gen/internal"
	"github.com/giantswarm/microerror"
	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "update resource.golden file")

// Test_Resource tests resource.go file rendering.
//
// It uses golden file as reference and when changes to template are
// intentional, they can be updated by providing -update flag for go test.
//
//	go test ./pkg/gen/resource -run Test_Resource -update
//
func Test_Resource(t *testing.T) {
	testCases := []struct {
		name         string
		dir          string
		group        string
		kind         string
		version      string
		errorMatcher func(err error) bool
	}{
		{
			name:         "case 0: core v1 ConfigMap resource.go",
			dir:          "/go/src/some.domain/project/subpath/configmapresource",
			group:        "core",
			kind:         "ConfigMap",
			version:      "v1",
			errorMatcher: nil,
		},
		{
			name:         "case 1: core v1 Secret resource.go",
			dir:          "/go/src/some.domain/project/subpath/secretresource",
			group:        "core",
			kind:         "Secret",
			version:      "v1",
			errorMatcher: nil,
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ctx := context.Background()

			c := Config{
				Dir:           tc.dir,
				ObjectGroup:   tc.group,
				ObjectKind:    tc.kind,
				ObjectVersion: tc.version,
			}

			f, err := NewResource(c)
			if err != nil {
				t.Fatal(microerror.Mask(err))
			}

			w := &bytes.Buffer{}
			err = internal.Execute(ctx, w, f)

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

			actual := w.Bytes()

			golden := filepath.Join("testdata", normalizeToFileName(tc.name)+".golden")
			if *update {
				ioutil.WriteFile(golden, actual, 0644)
			}

			expected, err := ioutil.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(actual, expected) {
				t.Fatalf("\n\n%s\n", cmp.Diff(actual, expected))
			}
		})
	}
}

// normalizeToFileName converts all non-digit, non-letter runes in input string
// to dash ('-'). Coalesces multiple dashes into one.
func normalizeToFileName(s string) string {
	var result []rune
	for _, r := range []rune(s) {
		if unicode.IsDigit(r) || unicode.IsLetter(r) {
			result = append(result, r)
		} else {
			l := len(result)
			if l > 0 && result[l-1] != '-' {
				result = append(result, rune('-'))
			}
		}
	}
	return string(result)
}
