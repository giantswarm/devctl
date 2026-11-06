package depclient_test

import (
	"context"
	"io"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/depclient"
)

func TestReadManifestProjectPruneOptions(t *testing.T) {
	c := newClient(t)
	testCases := []struct {
		name string
		data string
	}{
		{
			name: "case 0: array of tables",
			data: `[prune]
unused-packages = true
non-go = true
go-tests = true

[[prune.project]]
name = "example.com/first"
unused-packages = false
non-go = false

[[prune.project]]
name = "example.com/second"
go-tests = false
`,
		},
		{
			name: "case 1: inline tables",
			data: `[prune]
unused-packages = true
non-go = true
go-tests = true
project = [
  { name = "example.com/first", unused-packages = false, non-go = false },
  { name = "example.com/second", go-tests = false },
]
`,
		},
	}

	want := depclient.Manifest{
		PruneOptions: depclient.ManifestPruneOptions{
			UnusedPackages: true,
			NonGoFiles:     true,
			GoTests:        true,
			Projects: []map[string]interface{}{
				{"name": "example.com/first", "unused-packages": false, "non-go": false},
				{"name": "example.com/second", "go-tests": false},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.ReadManifest(context.Background(), []byte(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			// Omitted options must stay absent so callers can distinguish them
			// from an explicit false that overrides a global prune option.
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("manifest (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadManifestRejectsInvalidProjectPruneOptions(t *testing.T) {
	c := newClient(t)
	data := []byte(`required = ["example.com/tool"]
[[constraint]]
name = "example.com/dependency"
version = "1.0.0"
[prune]
non-go = true
project = "not an array of tables"
`)
	got, err := c.ReadManifest(context.Background(), data)
	if err == nil {
		t.Error("invalid prune.project: want a decoding error")
	}
	if diff := cmp.Diff(depclient.Manifest{}, got); diff != "" {
		t.Errorf("failed read must not return a partial manifest (-want +got):\n%s", diff)
	}
}

func newClient(t *testing.T) *depclient.Client {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	c, err := depclient.New(depclient.Config{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
