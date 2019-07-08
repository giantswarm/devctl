package resource

import (
	"context"
	"flag"
	"strconv"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/pkg/gen/internal"
	"github.com/giantswarm/microerror"
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
		errorMatcher func(err error) bool
	}{
		{
			name:         "case 0",
			errorMatcher: nil,
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ctx := context.Background()

			c := resource.Config{
				Dir:           "/does/not/matter/for/this/test",
				ObjectGroup:   "core",
				ObjectKind:    "ConfigMap",
				ObjectVersion: "v1",
			}

			f, err := resource.NewResource(c)
			if err != nil {
				return microerror.Mask(err)
			}

			var b strings.Builder
			err := internal.Execute(ctx, w, f)

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
