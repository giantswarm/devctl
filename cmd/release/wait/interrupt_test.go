//go:build unix

package wait

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/interrupttest"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

func Test_Run_interrupted(t *testing.T) {
	for _, sig := range interrupttest.Signals {
		t.Run(sig.String(), func(t *testing.T) {
			doc := interrupttest.Run(t, agentcli.ExitTimeout, func() error {
				t.Setenv(agentcli.EnvTimeScale, "0.001")
				sources := interrupttest.ReleaseSources(t, func(ctx context.Context) error { return interrupttest.Send(ctx, sig) })
				r := &runner{
					flag: &flag{Timeout: time.Hour}, stdout: os.Stdout, stderr: os.Stderr,
					gate: func(bool) error { return nil },
					open: func(context.Context, string, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
						return sources, nil
					},
				}
				return r.Run(&cobra.Command{}, []string{"o/r", "v1.2.3"})
			})
			pipeline := doc["pipeline"].(map[string]any)
			if doc["verdict"] != "timeout" || doc["tag"] != "v1.2.3" || !strings.Contains(doc["reason"].(string), "interrupted") || len(pipeline["unfinished"].([]any)) == 0 || len(doc["artifacts"].([]any)) != 2 {
				t.Fatalf("interrupted release lost its partial state: %v", doc)
			}
		})
	}
}
