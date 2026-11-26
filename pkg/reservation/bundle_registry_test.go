package reservation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
)

// TestBundleRegistryReadsAChartFromAnOCIRegistry: the default pull lists the
// tags of a chart and loads the chart layer, with no credentials.
func TestBundleRegistryReadsAChartFromAnOCIRegistry(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	ref := strings.TrimPrefix(srv.URL, "http://") + "/charts/demo-bundle"

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	chartYAML := "apiVersion: v2\nname: demo-bundle\nversion: 1.2.3\n"
	if err := tw.WriteHeader(&tar.Header{Name: "demo-bundle/Chart.yaml", Mode: 0o600, Size: int64(len(chartYAML))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(chartYAML)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := mutate.AppendLayers(empty.Image, static.NewLayer(buf.Bytes(), helmChartLayer))
	if err != nil {
		t.Fatal(err)
	}
	tag, err := name.NewTag(ref + ":1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(tag, img); err != nil {
		t.Fatal(err)
	}

	tags, err := bundleTags(ref)
	if err != nil || !slices.Equal(tags, []string{"1.2.3"}) {
		t.Fatalf("bundleTags = %v, %v; want [1.2.3]", tags, err)
	}
	c, err := pullBundle(ref, "1.2.3")
	if err != nil || c.Name() != "demo-bundle" {
		t.Fatalf("pullBundle = %v, %v; want the chart demo-bundle", c, err)
	}
	if _, err := pullBundle(ref, "9.9.9"); err == nil {
		t.Error("pullBundle of a missing tag succeeded")
	}
}
