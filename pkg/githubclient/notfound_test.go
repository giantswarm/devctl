package githubclient

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/giantswarm/microerror"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v92/github"
)

func TestExplainNotFound(t *testing.T) {
	const hint = "the App reaches giantswarm only"
	answer := func(status int) error {
		return &github.ErrorResponse{Response: &http.Response{StatusCode: status, Request: &http.Request{}}, Message: http.StatusText(status)}
	}
	testCases := []struct {
		name     string
		err      error
		hint     string
		explains bool
	}{
		{name: "API 404, masked", err: microerror.Mask(answer(http.StatusNotFound)), hint: hint, explains: true},
		{name: "git repository not found, masked", err: microerror.Mask(fmt.Errorf("%w: Repository not found.", transport.ErrRepositoryNotFound)), hint: hint, explains: true},
		{name: "API 403", err: answer(http.StatusForbidden), hint: hint},
		{name: "API 404, no hint (a token from the environment)", err: answer(http.StatusNotFound)},
		{name: "another error", err: errors.New("boom"), hint: hint},
		{name: "nil", hint: hint},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExplainNotFound(tc.err, tc.hint)
			if !tc.explains {
				if got != tc.err {
					t.Fatalf("ExplainNotFound = %v, want the error unchanged", got)
				}
				return
			}
			if !errors.Is(got, tc.err) {
				t.Errorf("ExplainNotFound does not wrap %v", tc.err)
			}
			if pretty := microerror.Pretty(got, false); !strings.HasSuffix(pretty, "; "+hint) {
				t.Errorf("microerror.Pretty = %q, want it to end with the hint", pretty)
			}
		})
	}
}
