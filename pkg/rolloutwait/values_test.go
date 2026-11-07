package rolloutwait

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// These are Flux's digests of the literal canonical YAML, independently of
// the production values resolver and encoder.
func valuesDigest(yaml string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(yaml))) }

func failedWithValues(running, attemptedValues string) *unstructured.Unstructured {
	hr := helmRelease(running, map[string]any{
		"lastAttemptedRevision":     "0.48.1",
		"lastAttemptedConfigDigest": valuesDigest(attemptedValues),
		"conditions":                ready("False", "UpgradeFailed", "old values failed"),
	})
	_ = unstructured.SetNestedSlice(hr.Object, []any{map[string]any{"kind": "ConfigMap", "name": "values"}}, "spec", "valuesFrom")
	return hr
}

func valuesConfigMap(yaml string) *unstructured.Unstructured {
	return obj("v1", "ConfigMap", "flux-giantswarm", "values", map[string]any{"data": map[string]any{"values.yaml": yaml}})
}

func TestStaleValuesFailureWaits(t *testing.T) {
	for _, named := range []bool{false, true} {
		for _, reconcile := range []bool{false, true} {
			t.Run(fmt.Sprintf("named=%t/reconcile=%t", named, reconcile), func(t *testing.T) {
				hr := failedWithValues("0.48.1", "replicas: 1\n")
				client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), hr, valuesConfigMap("replicas: 2\n"))
				r := wait(t, client, reconcile, func(c *Config) {
					if named {
						c.Charts = []string{"meta-chart"}
						c.HelmReleases = []string{"flux-giantswarm/" + chart}
					}
				})
				wantExit(t, r, agentcli.ExitTimeout, "values have changed since the last attempt")
				patches := 0
				for _, a := range client.Actions() {
					if p, ok := a.(k8stesting.PatchAction); ok {
						patches++
						if p.GetResource() != helmReleases || p.GetName() != chart || !strings.Contains(string(p.GetPatch()), "reconcile.fluxcd.io/requestedAt") {
							t.Errorf("unexpected patch: %+v", p)
						}
					}
				}
				wantPatches := 0
				if reconcile {
					wantPatches = 1
				}
				if patches != wantPatches {
					t.Errorf("want %d HelmRelease patches, got %d", wantPatches, patches)
				}
			})
		}
	}
}

func TestStaleValuesFailureSettles(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		for _, succeeds := range []bool{false, true} {
			t.Run(fmt.Sprintf("reconcile=%t/succeeds=%t", reconcile, succeeds), func(t *testing.T) {
				hr := failedWithValues("0.48.0", "replicas: 1\n")
				client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), hr, valuesConfigMap("replicas: 2\n"), deployment(2))
				requested := false
				client.PrependReactor("patch", "helmreleases", func(k8stesting.Action) (bool, runtime.Object, error) {
					if requested {
						t.Error("reconcile was requested more than once")
					}
					requested = true
					return false, nil, nil
				})
				polls := 0
				client.PrependReactor("list", "helmreleases", func(k8stesting.Action) (bool, runtime.Object, error) {
					polls++
					if polls == 3 {
						if reconcile && !requested {
							t.Fatal("new attempt before --reconcile requested it")
						}
						_ = unstructured.SetNestedField(hr.Object, valuesDigest("replicas: 2\n"), "status", "lastAttemptedConfigDigest")
						conditions := ready("False", "UpgradeFailed", "new values failed")
						if succeeds {
							conditions = ready("True", "UpgradeSucceeded", "")
							hr.Object["status"] = helmRelease("0.48.1", nil).Object["status"]
						}
						_ = unstructured.SetNestedSlice(hr.Object, conditions, "status", "conditions")
					}
					return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*hr.DeepCopy()}}, nil
				})
				r := wait(t, client, reconcile)
				code, reason := agentcli.ExitRed, "new values failed"
				if succeeds {
					code, reason = agentcli.ExitOK, ""
				}
				wantExit(t, r, code, reason)
				if polls != 3 {
					t.Errorf("want three polls, got %d", polls)
				}
				if requested != reconcile {
					t.Errorf("reconcile requested=%t, want %t", requested, reconcile)
				}
				sourcePatches := 0
				for _, a := range client.Actions() {
					if a.GetVerb() == "patch" && a.GetResource() == ociRepositories {
						sourcePatches++
					}
				}
				wantSources := 0
				if reconcile {
					wantSources = 1
				}
				if sourcePatches != wantSources {
					t.Errorf("want %d source patches while behind the version, got %d", wantSources, sourcePatches)
				}
			})
		}
	}
}

func TestCurrentValuesFailureAtDeployedVersion(t *testing.T) {
	hr := failedWithValues("0.48.1", "replicas: 2\n")
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), hr, valuesConfigMap("replicas: 2\n")), false)
	wantExit(t, r, agentcli.ExitRed, "old values failed")
}

func TestHelmValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		refs   []any
		inline map[string]any
		want   map[string]any
	}{
		{
			name: "case 0: ordered references with recursive inline overrides",
			refs: []any{
				map[string]any{"kind": "ConfigMap", "name": "values"},
				map[string]any{"kind": "Secret", "name": "values", "valuesKey": "overrides"},
			},
			inline: map[string]any{"nested": map[string]any{"inline": true}},
			want:   map[string]any{"nested": map[string]any{"first": float64(1), "overridden": "secret", "inline": true}, "list": []any{"second"}},
		},
		{
			name: "case 1: optional missing resource and key",
			refs: []any{
				map[string]any{"kind": "ConfigMap", "name": "absent", "optional": true},
				map[string]any{"kind": "ConfigMap", "name": "values", "valuesKey": "absent", "optional": true},
			},
			inline: map[string]any{"inline": "only"}, want: map[string]any{"inline": "only"},
		},
		{
			name: "case 2: targetPath uses Helm's list and escaped-key syntax",
			refs: []any{map[string]any{"kind": "Secret", "name": "values", "valuesKey": "scalar", "targetPath": `nested.items[0].some\.key`}},
			want: map[string]any{"nested": map[string]any{"items": []any{map[string]any{"some.key": int64(2)}}}},
		},
		{
			name: "case 3: quoted targetPath value stays a string",
			refs: []any{map[string]any{"kind": "Secret", "name": "values", "valuesKey": "quoted", "targetPath": "nested.value"}},
			want: map[string]any{"nested": map[string]any{"value": "2"}},
		},
		{
			name: "case 4: literal targetPath preserves metacharacters",
			refs: []any{map[string]any{"kind": "Secret", "name": "values", "valuesKey": "literal", "targetPath": `nested.some\.key`, "literal": true}},
			want: map[string]any{"nested": map[string]any{"some.key": `a,b=[c]{d}\e`}},
		},
		{
			name: "case 5: later reference cannot mutate inline defaults",
			refs: []any{
				map[string]any{"kind": "Secret", "name": "values", "valuesKey": "scalar", "targetPath": "nested.value"},
				map[string]any{"kind": "Secret", "name": "values", "valuesKey": "overrides"},
			},
			inline: map[string]any{"nested": map[string]any{"overridden": "inline"}},
			want:   map[string]any{"nested": map[string]any{"overridden": "inline", "value": int64(2)}, "list": []any{"second"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hr := helmRelease("", nil)
			_ = unstructured.SetNestedSlice(hr.Object, tc.refs, "spec", "valuesFrom")
			_ = unstructured.SetNestedMap(hr.Object, tc.inline, "spec", "values")
			secret := obj("v1", "Secret", "flux-giantswarm", "values", map[string]any{"data": map[string]any{
				"overrides": base64.StdEncoding.EncodeToString([]byte("nested:\n  overridden: secret\nlist: [second]\n")),
				"scalar":    base64.StdEncoding.EncodeToString([]byte("2")),
				"quoted":    base64.StdEncoding.EncodeToString([]byte("'2'")),
				"literal":   base64.StdEncoding.EncodeToString([]byte(`a,b=[c]{d}\e`)),
			}})
			client := newClient(valuesConfigMap("nested:\n  first: 1\n  overridden: first\nlist: [first]\n"), secret)
			w := &Waiter{client: client}
			got, err := w.helmValues(context.Background(), hr)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("values (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValuesFailureDigest(t *testing.T) {
	for _, tc := range []struct {
		name, input, canonical, digest string
	}{
		{name: "case 0: YAML layout and map ordering do not change inputs", input: "{replicas: 2}", canonical: "replicas: 2\n"},
		{name: "case 1: lexicographic ordering including maps inside lists", input: "key2: 2\nkey10: 10\nitems:\n- z2: two\n  z10: ten\n", canonical: "items:\n- z10: ten\n  z2: two\nkey10: 10\nkey2: 2\n"},
		{name: "case 2: empty values hash zero bytes", input: "{}", canonical: ""},
		// Independently calculated SHA1 of the literal YAML "replicas: 2\n".
		{name: "case 3: legacy checksum", input: "replicas: 2\n", digest: "96bc5f992b874ad84925a2f144739ecfcc2621b0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hr := failedWithValues("0.48.0", tc.canonical)
			if tc.digest != "" {
				unstructured.RemoveNestedField(hr.Object, "status", "lastAttemptedConfigDigest")
				_ = unstructured.SetNestedField(hr.Object, tc.digest, "status", "lastAttemptedValuesChecksum")
			}
			r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), hr, valuesConfigMap(tc.input)), false)
			wantExit(t, r, agentcli.ExitRed, "old values failed")
		})
	}
}

func TestValuesFailureReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"case 0: forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "values", fmt.Errorf("denied")), agentcli.ExitUsage},
		{"case 1: unauthorized", apierrors.NewUnauthorized("expired"), agentcli.ExitAuthRequired},
		{"case 2: missing required values", apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "values"), agentcli.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), failedWithValues("0.48.0", "replicas: 1\n"))
			client.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, tc.err })
			r := wait(t, client, true)
			wantExit(t, r, tc.code, "")
			for _, a := range client.Actions() {
				if a.GetVerb() == "patch" {
					t.Error("must not reconcile after a refused input read")
				}
			}
		})
	}
}

func TestValuesFailureParseErrorsOmitData(t *testing.T) {
	for _, target := range []string{"", "nested.value"} {
		t.Run("target="+target, func(t *testing.T) {
			hr := failedWithValues("0.48.0", "replicas: 1\n")
			_ = unstructured.SetNestedSlice(hr.Object, []any{map[string]any{"kind": "Secret", "name": "values", "targetPath": target}}, "spec", "valuesFrom")
			// Synthetic secret data deliberately rejected by YAML and strvals.
			const data = "{private-fixture: [broken"
			secret := obj("v1", "Secret", "flux-giantswarm", "values", map[string]any{"data": map[string]any{"values.yaml": base64.StdEncoding.EncodeToString([]byte(data))}})
			r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), hr, secret), false)
			wantExit(t, r, agentcli.ExitUsage, "cannot parse")
			if strings.Contains(r.err.Error(), "private-fixture") || strings.Contains(r.err.Error(), "broken") {
				t.Fatalf("parse error disclosed input data: %v", r.err)
			}
		})
	}
}

func TestValuesChangeAgainBeforeAttempt(t *testing.T) {
	hr := failedWithValues("0.48.0", "replicas: 1\n")
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), hr)
	polls := 0
	client.PrependReactor("list", "helmreleases", func(k8stesting.Action) (bool, runtime.Object, error) {
		polls++
		if polls > 1 {
			_ = unstructured.SetNestedField(hr.Object, valuesDigest(fmt.Sprintf("replicas: %d\n", polls)), "status", "lastAttemptedConfigDigest")
			_ = unstructured.SetNestedSlice(hr.Object, ready("False", "UpgradeFailed", fmt.Sprintf("attempt %d failed", polls)), "status", "conditions")
		}
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*hr.DeepCopy()}}, nil
	})
	client.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, valuesConfigMap(fmt.Sprintf("replicas: %d\n", min(polls+1, 3))), nil
	})
	r := wait(t, client, false)
	wantExit(t, r, agentcli.ExitRed, "attempt 3 failed")
	if polls != 3 {
		t.Errorf("want three polls, got %d", polls)
	}
}
