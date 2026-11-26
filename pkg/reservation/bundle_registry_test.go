package reservation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// chartArchive is a chart named demo-bundle as a registry layer holds it.
func chartArchive(t *testing.T) []byte {
	t.Helper()

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

	return buf.Bytes()
}

// push writes an image with one layer of the given type to ref:tag of a new
// in-process registry, and returns ref.
func push(t *testing.T, tag string, mediaType types.MediaType) string {
	t.Helper()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	ref := strings.TrimPrefix(srv.URL, "http://") + "/charts/demo-bundle"
	img, err := mutate.AppendLayers(empty.Image, static.NewLayer(chartArchive(t), mediaType))
	if err != nil {
		t.Fatal(err)
	}
	tagRef, err := name.NewTag(ref + ":" + tag)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(tagRef, img); err != nil {
		t.Fatal(err)
	}

	return ref
}

// TestBundleRegistryReadsAChartFromAnOCIRegistry: the default pull lists the
// tags of a chart and loads the chart layer, with no credentials.
func TestBundleRegistryReadsAChartFromAnOCIRegistry(t *testing.T) {
	ref := push(t, "1.2.3", helmChartLayer)

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

// TestBundleRegistryMapsPlusToUnderscore: an OCI tag cannot hold `+`, so a
// version 1.2.3+build is the tag 1.2.3_build. Listing gives the version, and a
// pull by the version reads the tag.
func TestBundleRegistryMapsPlusToUnderscore(t *testing.T) {
	ref := push(t, "1.2.3_build", helmChartLayer)

	tags, err := bundleTags(ref)
	if err != nil || !slices.Equal(tags, []string{"1.2.3+build"}) {
		t.Fatalf("bundleTags = %v, %v; want [1.2.3+build]", tags, err)
	}
	if _, err := pullBundle(ref, "1.2.3+build"); err != nil {
		t.Errorf("pullBundle of 1.2.3+build: %v", err)
	}
}

func TestBundleRegistryRefusesAnImageWithNoChartLayer(t *testing.T) {
	ref := push(t, "1.2.3", "application/octet-stream")

	if _, err := pullBundle(ref, "1.2.3"); !IsInvalidConfig(err) {
		t.Errorf("want an invalid configuration, got %v", err)
	}
}

// TestBundleRegistryTimesOut: a registry that accepts the connection and never
// answers must not block the read.
func TestBundleRegistryTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })
	ref := strings.TrimPrefix(srv.URL, "http://") + "/charts/demo-bundle"
	old := bundleRegistryTimeout
	bundleRegistryTimeout = 100 * time.Millisecond
	t.Cleanup(func() { bundleRegistryTimeout = old })

	if _, err := bundleTags(ref); err == nil {
		t.Error("bundleTags of a stalled registry succeeded")
	}
	if _, err := pullBundle(ref, "1.2.3"); err == nil {
		t.Error("pullBundle of a stalled registry succeeded")
	}
}
