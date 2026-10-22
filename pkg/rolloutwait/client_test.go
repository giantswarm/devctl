package rolloutwait

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// apiServer is a management cluster's API that lists objects, for the
// person whose bearer token it accepts.
func apiServer(t *testing.T, token string, objects ...*unstructured.Unstructured) *httptest.Server {
	t.Helper()
	listKinds := map[string]string{}
	for resource, kind := range ListKinds() {
		listKinds[resource.Resource] = kind
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// /api/v1/[namespaces/<ns>/]<resource> or
		// /apis/<group>/<version>/[namespaces/<ns>/]<resource>
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if parts[0] == "apis" {
			parts = parts[3:]
		} else {
			parts = parts[2:]
		}
		namespace := ""
		if len(parts) == 3 && parts[0] == "namespaces" {
			namespace, parts = parts[1], parts[2:]
		}
		if r.Method != http.MethodGet || len(parts) != 1 {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		items := []any{}
		for _, o := range objects {
			if o.GetKind()+"List" == listKinds[parts[0]] && (namespace == "" || o.GetNamespace() == namespace) {
				items = append(items, o.Object)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": listKinds[parts[0]], "metadata": map[string]any{}, "items": items})
	}))
	t.Cleanup(server.Close)
	return server
}

// TestRolledOutThroughATokenBuiltClient waits on a client built from the
// bearer token a server holds for the person: no kubeconfig is read.
func TestRolledOutThroughATokenBuiltClient(t *testing.T) {
	server := apiServer(t, "person-token", ociRepository(map[string]any{"semver": ">=0.17.0 <1.0.0"}), helmRelease("0.48.1", nil), deployment(2))
	wrapped := 0
	client, err := NewClient(&rest.Config{Host: server.URL, BearerToken: "person-token"}, func(rt http.RoundTripper) http.RoundTripper {
		wrapped++
		return rt
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(Config{Installation: "myinstallation", Version: "v0.48.1", Charts: []string{chart}, Client: client, Clock: agentcli.NewVirtualClock(time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	result := NewResult("myinstallation", "")
	if err := w.Wait(t.Context(), &result); agentcli.Exit(err) != agentcli.ExitOK {
		t.Fatalf("want exit 0, got %d (%v)", agentcli.Exit(err), err)
	}
	if len(result.Deployments) != 1 || result.Deployments[0].State != StateRolledOut {
		t.Errorf("deployments: %+v", result.Deployments)
	}
	if wrapped == 0 {
		t.Error("the requests go through wrap")
	}
}

func TestATokenBuiltClientTheClusterRefusesIsExit8(t *testing.T) {
	server := apiServer(t, "person-token")
	client, err := NewClient(&rest.Config{Host: server.URL, BearerToken: "another-token"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(Config{Installation: "myinstallation", Version: "v0.48.1", Charts: []string{chart}, Client: client, Clock: agentcli.NewVirtualClock(time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	result := NewResult("myinstallation", "")
	if err := w.Wait(t.Context(), &result); agentcli.Exit(err) != agentcli.ExitAuthRequired {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitAuthRequired, agentcli.Exit(err), err)
	}
}
