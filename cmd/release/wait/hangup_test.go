//go:build !windows

package wait

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/hanguptest"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

type hangupRegistry struct {
	releasewait.Prober
	hangup func()
}

func (r hangupRegistry) Probe(ctx context.Context, a releasewait.Artifact) (string, bool, error) {
	r.hangup()
	return r.Prober.Probe(ctx, a)
}

func Test_Run_survivesHangupAndClosedStdin(t *testing.T) {
	stdout := hanguptest.Run(t, agentcli.ExitOK, func(hangup func()) error {
		image := "gsoci.azurecr.io/giantswarm/devctl:1.2.3"
		registry := &releasewaittest.Registry{Missing: map[string]int{image: 1}}
		sources, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", registry, "success")
		if err != nil {
			t.Fatal(err)
		}
		sources.Registry = hangupRegistry{Prober: registry, hangup: hangup}
		r := &runner{
			gate:   func(bool) error { return nil },
			flag:   &flag{Timeout: 2 * time.Minute},
			stdout: os.Stdout,
			stderr: io.Discard,
			open: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
				return sources, nil
			},
		}
		err = r.Run(&cobra.Command{}, []string{"giantswarm/devctl", "v1.2.3"})
		if registry.Probes(image) < 2 {
			t.Error("the wait did not poll again after SIGHUP")
		}
		return err
	})
	var doc releasewait.Document
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("stdout is not one document: %v\n%s", err, stdout)
	}
	if doc.Command != releasewait.Command || doc.ExitCode != agentcli.ExitOK || doc.Verdict != agentcli.VerdictAvailable || doc.Tag != "v1.2.3" || len(doc.Artifacts) != 2 {
		t.Fatalf("want the available release document after SIGHUP, got %+v", doc)
	}
}
