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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/internal/hanguptest"
	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

func Test_Run_survivesHangupAndClosedStdin(t *testing.T) {
	stdout := hanguptest.Run(t, agentcli.ExitOK, func(hangup func()) error {
		sources, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, "success")
		if err != nil {
			t.Fatal(err)
		}
		cluster := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds())
		polls := 0
		cluster.PrependReactor("list", "apps", func(k8stesting.Action) (bool, runtime.Object, error) {
			polls++
			hangup()
			running := "1.2.2"
			if polls > 1 {
				running = "1.2.3"
			}
			return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{Object: map[string]any{
				"apiVersion": "application.giantswarm.io/v1alpha1", "kind": "App",
				"metadata": map[string]any{"name": "devctl", "namespace": "giantswarm"},
				"spec": map[string]any{
					"name": "devctl", "version": "1.2.3", "namespace": "giantswarm",
					"kubeConfig": map[string]any{"inCluster": true},
				},
				"status": map[string]any{"version": running, "release": map[string]any{"status": "deployed"}},
			}}}}, nil
		})
		r := &runner{
			gate:   func(bool) error { return nil },
			flag:   &flag{Timeout: 2 * time.Minute, ReleaseTimeout: 2 * time.Minute},
			stdout: os.Stdout,
			stderr: io.Discard,
			openRelease: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
				return sources, nil
			},
			openCluster: func(context.Context, laptop.ClusterConfig) (dynamic.Interface, error) {
				return cluster, nil
			},
		}
		err = r.Run(&cobra.Command{}, []string{"myinstallation", "giantswarm/devctl", "v1.2.3"})
		if polls < 2 {
			t.Error("the rollout did not poll again after SIGHUP")
		}
		return err
	})
	var doc rolloutwait.Document
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("stdout is not one document: %v\n%s", err, stdout)
	}
	if doc.Command != rolloutwait.Command || doc.ExitCode != agentcli.ExitOK || doc.Verdict != rolloutwait.VerdictRolledOut || doc.Release.Verdict != agentcli.VerdictAvailable || len(doc.Deployments) != 1 {
		t.Fatalf("want the rolled out document after SIGHUP, got %+v", doc)
	}
}
