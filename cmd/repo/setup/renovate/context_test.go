package renovate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Cancellation while the repository read finishes must stop the subsequent
// permission change. The transport honors cancellation without any waiting.
func TestRunPreservesCommandCancellation(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprintf("remove=%t", remove), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requests []string
			command, log := newTestCommand(t, func(req *http.Request) (*http.Response, error) {
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				requests = append(requests, req.Method+" "+req.URL.Path)
				if req.Method == http.MethodGet {
					cancel()
					return testResponse(req, http.StatusOK, `{"id":42,"name":"repo"}`), nil
				}
				return testResponse(req, http.StatusNoContent, ""), nil
			})
			args := []string{"renovate", "giantswarm/repo"}
			if remove {
				args = append(args, "--remove")
			}
			command.SetArgs(args)
			if err := command.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
				t.Errorf("want context.Canceled, got %v", err)
			}
			if want := []string{"GET /repos/giantswarm/repo"}; !slices.Equal(requests, want) {
				t.Errorf("requests = %v, want %v", requests, want)
			}
			if strings.Contains(log.String(), "accessible by Renovate") {
				t.Errorf("canceled mutation logged success: %s", log.String())
			}
		})
	}
}
