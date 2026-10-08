package list_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/giantswarm/devctl/v8/cmd/reservation/list"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

const cluster = "graveler"

// newCommand builds the list command the way cmd/reservation does, with a
// fake cluster that holds data as the reservations ConfigMap, or no ConfigMap
// when data is nil. It fails the test when the command opens another context
// than the default one.
func newCommand(t *testing.T, stdout io.Writer, data map[string]any) *cobra.Command {
	t.Helper()

	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}

	var objects []runtime.Object
	if data != nil {
		objects = append(objects, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "reservations", "namespace": "giantswarm"},
			"data":       data,
		}})
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{{Version: "v1", Resource: "configmaps"}: "ConfigMapList"}, objects...)

	cmd, err := list.New(list.Config{
		Logger: logger,
		Stderr: io.Discard,
		Stdout: stdout,
		OpenCluster: func(c, kubeContext string) (dynamic.Interface, error) {
			if want := rolloutwait.ContextPrefix + cluster; c != cluster || kubeContext != want {
				t.Errorf("opened %s through %s, want %s through %s", c, kubeContext, cluster, want)
			}
			return client, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

func entry(user, until string) string {
	return "{user: " + user + ", branch: fix/crash, pr: giantswarm/hello-world#123, scope: app, from: 2026-09-15T10:00:00Z, until: " + until + "}"
}

// TestRefusesAMissingCluster checks that a missing --cluster is refused by the
// flags, before the command opens a cluster.
func TestRefusesAMissingCluster(t *testing.T) {
	cmd := newCommand(t, io.Discard, nil)

	err := cmd.Execute()
	if !list.IsInvalidFlag(err) {
		t.Fatalf("expected an invalid-flag error, got %v", err)
	}
}

// TestListPrintsTheReservationFields reads the ConfigMap on the cluster, no
// checkout and no GitHub token, and prints the fields user story 26 asks for.
func TestListPrintsTheReservationFields(t *testing.T) {
	until := time.Now().Add(10 * time.Hour).UTC()
	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout, map[string]any{"hello-world": entry("alice", until.Format(time.RFC3339))})
	cmd.SetArgs([]string{"--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{"hello-world", "alice", "fix/crash", "giantswarm/hello-world#123", "app", until.Format("2006-01-02")} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

// TestListExcludesAnExpiredReservation is the "active" qualifier on user
// story 26: an entry whose Until has already passed is not active, whether or
// not the reaper has swept it yet.
func TestListExcludesAnExpiredReservation(t *testing.T) {
	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout, map[string]any{
		"hello-world": entry("alice", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)),
		"other-app":   entry("bob", time.Now().Add(10*time.Hour).UTC().Format(time.RFC3339)),
	})
	cmd.SetArgs([]string{"--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	out := stdout.String()
	if strings.Contains(out, "\nhello-world ") {
		t.Errorf("output mentions the expired app:\n%s", out)
	}
	if !strings.Contains(out, "other-app") {
		t.Errorf("output does not mention the active app:\n%s", out)
	}
}

// TestListReportsNoneOnAClusterWithNoReservations checks the empty case reads
// as "nothing to see", not a blank table, and names the Flux lag.
func TestListReportsNoneOnAClusterWithNoReservations(t *testing.T) {
	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout, map[string]any{})
	cmd.SetArgs([]string{"--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"No active reservations", "Flux"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output does not mention %q: %s", want, stdout.String())
		}
	}
}

// TestListRefusesAClusterWithoutTheConfigMap checks a cluster that is not
// enabled is an error, not an empty list.
func TestListRefusesAClusterWithoutTheConfigMap(t *testing.T) {
	cmd := newCommand(t, io.Discard, nil)
	cmd.SetArgs([]string{"--cluster", cluster})

	err := cmd.Execute()
	if !list.IsClusterNotEnabled(err) {
		t.Fatalf("expected a cluster-not-enabled error, got %v", err)
	}
}
