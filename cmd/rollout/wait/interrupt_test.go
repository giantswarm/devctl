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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/internal/interrupttest"
	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

func Test_Run_interrupted(t *testing.T) {
	for _, phase := range []string{"release", "rollout"} {
		t.Run(phase, func(t *testing.T) {
			for _, sig := range interrupttest.Signals {
				t.Run(sig.String(), func(t *testing.T) {
					doc := interrupttest.Run(t, agentcli.ExitTimeout, func() error {
						t.Setenv(agentcli.EnvTimeScale, "0.001")
						interrupt := func(ctx context.Context) error { return interrupttest.Send(ctx, sig) }
						var releaseInterrupt func(context.Context) error
						if phase == "release" {
							releaseInterrupt = interrupt
						}
						sources := interrupttest.ReleaseSources(t, releaseInterrupt)
						app := &unstructured.Unstructured{Object: map[string]any{
							"apiVersion": "application.giantswarm.io/v1alpha1", "kind": "App",
							"metadata": map[string]any{"name": "r", "namespace": "org-test", "generation": int64(1)},
							"spec":     map[string]any{"name": "r", "version": "1.2.3", "catalog": "control-plane-catalog", "namespace": "test", "kubeConfig": map[string]any{"inCluster": false}},
							"status":   map[string]any{"version": "1.2.2", "release": map[string]any{"status": "deployed"}},
						}}
						client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds(), app)
						r := &runner{
							flag: &flag{Timeout: time.Hour, ReleaseTimeout: time.Hour}, stdout: os.Stdout, stderr: os.Stderr,
							gate: func(bool) error { return nil },
							openRelease: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
								return sources, nil
							},
							openCluster: func(ctx context.Context, _ laptop.ClusterConfig) (dynamic.Interface, error) {
								reads := 0
								client.PrependReactor("list", "apps", func(k8stesting.Action) (bool, runtime.Object, error) {
									reads++
									if reads == 2 {
										return true, nil, interrupt(ctx)
									}
									return false, nil, nil
								})
								return client, nil
							},
						}
						return r.Run(&cobra.Command{}, []string{"test", "o/r", "v1.2.3"})
					})
					if doc["verdict"] != "timeout" || !strings.Contains(doc["reason"].(string), "interrupted") {
						t.Fatalf("interruption document: %v", doc)
					}
					release := doc["release"].(map[string]any)
					if phase == "release" {
						if release["verdict"] != "timeout" || len(release["pipeline"].(map[string]any)["unfinished"].([]any)) == 0 {
							t.Fatalf("interrupted release state: %v", doc)
						}
					} else if release["verdict"] != "available" || len(doc["deployments"].([]any)) != 1 {
						t.Fatalf("interrupted rollout lost its pending App: %v", doc)
					}
				})
			}
		})
	}
}
