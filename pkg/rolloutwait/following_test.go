package rolloutwait

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// The first two polls see the old GitOps selector. Once it changes, Flux
// still has to deploy the version and roll its workloads out. The last
// poll is at the original deadline: neither phase gets another timeout.
func TestVersionSelectorCatchesUp(t *testing.T) {
	chartSource := obj("source.toolkit.fluxcd.io/v1", kindHelmChart, "flux-giantswarm", chart, map[string]any{
		"spec": map[string]any{"chart": chart, "version": "0.47.0"},
	})
	chartRelease := helmRelease("0.47.0", nil)
	_ = unstructured.SetNestedField(chartRelease.Object, kindHelmChart, "spec", "chartRef", "kind")
	templateRelease := helmRelease("0.47.0", nil)
	unstructured.RemoveNestedField(templateRelease.Object, "spec", "chartRef")
	_ = unstructured.SetNestedMap(templateRelease.Object, map[string]any{
		"chart": chart, "version": "0.47.0", "sourceRef": map[string]any{"kind": "HelmRepository", "name": "control-plane-catalog"},
	}, "spec", "chart", "spec")
	a := app("giantswarm", chart, "0.47.0", "0.47.0", "deployed", true)
	testCases := []struct {
		name      string
		resource  schema.GroupVersionResource
		selector  *unstructured.Unstructured
		release   *unstructured.Unstructured
		path      []string
		following any
		patches   int
	}{
		{"case 0: OCI semver", ociRepositories, ociRepository(map[string]any{"semver": "0.47.0"}), helmRelease("0.47.0", nil), []string{"spec", "ref"}, map[string]any{"semver": "0.48.1"}, 2},
		{"case 1: OCI tag", ociRepositories, ociRepository(map[string]any{"tag": "0.47.0"}), helmRelease("0.47.0", nil), []string{"spec", "ref"}, map[string]any{"tag": "0.48.1"}, 2},
		{"case 2: OCI digest replaced by tag", ociRepositories, ociRepository(map[string]any{"digest": "sha256:abc"}), helmRelease("0.47.0", nil), []string{"spec", "ref"}, map[string]any{"tag": "0.48.1"}, 2},
		{"case 3: HelmChart", helmCharts, chartSource, chartRelease, []string{"spec", "version"}, "0.48.1", 2},
		{"case 4: chart template", helmReleases, templateRelease, templateRelease, []string{"spec", "chart", "spec", "version"}, "0.48.1", 1},
		{"case 5: App", apps, a, a, []string{"spec", "version"}, "0.48.1", 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objects := []runtime.Object{tc.selector, deployment(1)}
			if tc.selector != tc.release {
				objects = append(objects, tc.release)
			}
			client := newClient(objects...)
			polls := 0
			client.PrependReactor("list", "ocirepositories", func(k8stesting.Action) (bool, runtime.Object, error) {
				polls++
				switch polls {
				case 3:
					if err := unstructured.SetNestedField(tc.selector.Object, tc.following, tc.path...); err != nil {
						t.Fatal(err)
					}
					if err := client.Tracker().Update(tc.resource, tc.selector, tc.selector.GetNamespace()); err != nil {
						t.Fatal(err)
					}
				case 4:
					resource := helmReleases
					released := helmRelease("0.48.1", nil)
					if tc.release.GetKind() == KindApp {
						resource = apps
						released = app("giantswarm", chart, "0.48.1", "0.48.1", "deployed", true)
					}
					tc.release.Object["status"] = released.Object["status"]
					if err := client.Tracker().Update(resource, tc.release, tc.release.GetNamespace()); err != nil {
						t.Fatal(err)
					}
				case 5:
					if err := client.Tracker().Update(deployments, deployment(2), "agent-platform"); err != nil {
						t.Fatal(err)
					}
				}
				return false, nil, nil
			})
			client.PrependReactor("patch", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
				if polls < 3 {
					t.Fatal("reconciled before the source admitted the version")
				}
				return false, nil, nil
			})
			clock := agentcli.NewVirtualClock(time.Now())
			start := clock.Now()
			var progress bytes.Buffer
			r := wait(t, client, true, func(c *Config) {
				c.Clock = clock
				c.Progress = agentcli.NewProgress(&progress, true)
			})
			wantExit(t, r, agentcli.ExitOK, "")
			if polls != 5 || clock.Now().Sub(start) != time.Minute {
				t.Errorf("want 5 polls within one minute, got %d in %s", polls, clock.Now().Sub(start))
			}
			if len(r.result.Deployments) != 1 || r.result.Deployments[0].State != StateRolledOut || r.result.Deployments[0].RunningVersion != "0.48.1" {
				t.Errorf("result: %+v", r.result)
			}
			if len(r.warnings) != 0 {
				t.Errorf("stale warnings after the selector caught up: %v", r.warnings)
			}
			if !strings.Contains(progress.String(), "1 of 2 replicas updated") {
				t.Errorf("did not wait for workloads: %s", progress.String())
			}
			patches := 0
			for _, action := range client.Actions() {
				if action.GetVerb() == "patch" {
					patches++
				}
			}
			if patches != tc.patches {
				t.Errorf("want %d reconcile requests, got %d", tc.patches, patches)
			}
		})
	}
}

func TestFollowingVersionKeepsTheOriginalTimeout(t *testing.T) {
	client := newClient(helmRelease("0.47.0", nil))
	polls := 0
	client.PrependReactor("list", "ocirepositories", func(k8stesting.Action) (bool, runtime.Object, error) {
		polls++
		version := "0.47.0"
		if polls >= 3 {
			version = "0.48.1"
		}
		list := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "OCIRepositoryList"}}
		list.Items = []unstructured.Unstructured{*ociRepository(map[string]any{"semver": version})}
		return true, list, nil
	})
	clock := agentcli.NewVirtualClock(time.Now())
	start := clock.Now()
	r := wait(t, client, false, func(c *Config) { c.Clock = clock })
	wantExit(t, r, agentcli.ExitTimeout, "waiting for Flux to deploy 0.48.1")
	if clock.Now().Sub(start) != time.Minute {
		t.Errorf("selector and rollout must share one timeout, got %s", clock.Now().Sub(start))
	}
	if r.result.Deployments[0].State != StateProgressing || len(r.warnings) != 0 {
		t.Errorf("result: %+v; warnings: %v", r.result, r.warnings)
	}
}

func TestNotFollowingWaitCanBeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newClient(ociRepository(map[string]any{"semver": "0.47.0"}), helmRelease("0.47.0", nil))
	polls := 0
	client.PrependReactor("list", "ocirepositories", func(k8stesting.Action) (bool, runtime.Object, error) {
		polls++
		if polls == 2 {
			cancel()
		}
		return false, nil, nil
	})
	w, err := New(Config{
		Version: "0.48.1", Charts: []string{chart}, Client: client,
		Clock: agentcli.NewVirtualClock(time.Now()), Timeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := NewResult("myinstallation", "context")
	if err := w.Wait(ctx, &result); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation while waiting for the selector, got %v", err)
	}
	if polls != 2 {
		t.Errorf("want cancellation on poll 2, got %d polls", polls)
	}
}
