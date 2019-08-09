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

	"github.com/giantswarm/devctl/pkg/gen/input"
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
	configCoreV1ConfigMap := Config{
		Dir:           "/go/src/some.domain/project/subpath/configmapresource",
		ObjectGroup:   "core",
		ObjectKind:    "ConfigMap",
		ObjectVersion: "v1",
	}

	configG8sV2AWSConfig := Config{
		Dir:           "/go/src/some.domain/project/subpath/secretresource",
		ObjectGroup:   "g8s",
		ObjectKind:    "AWSConfig",
		ObjectVersion: "v2",
	}

	testCases := []struct {
		name         string
		inputConfig  Config
		newFileFunc  func(Config) (input.File, error)
		errorMatcher func(err error) bool
	}{
		{
			name:         "case 0: core v1 ConfigMap resource.go",
			inputConfig:  configCoreV1ConfigMap,
			newFileFunc:  func(c Config) (input.File, error) { return NewResource(c) },
			errorMatcher: nil,
		},
		{
			name:         "case 1: g8s v2 AWSConfig resource.go",
			inputConfig:  configG8sV2AWSConfig,
			newFileFunc:  func(c Config) (input.File, error) { return NewResource(c) },
			errorMatcher: nil,
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ctx := context.Background()

			f, err := tc.newFileFunc(tc.inputConfig)
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
