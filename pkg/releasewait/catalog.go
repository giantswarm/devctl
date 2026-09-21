package releasewait

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvCatalogURL points --catalog at another host for the catalog indexes;
// the default is the GitHub Pages host of the catalogs.
const EnvCatalogURL = "DEVCTL_CATALOG_URL"

// DefaultCatalogURL is where the catalog indexes are published:
// <DefaultCatalogURL>/<catalog>/index.yaml.
const DefaultCatalogURL = "https://giantswarm.github.io"

// CatalogIndex reads a catalog's index.yaml over HTTP.
type CatalogIndex struct {
	// BaseURL is the host of the indexes; empty means DefaultCatalogURL.
	BaseURL string
	// HTTPClient sends the requests; nil means the default client.
	HTTPClient *http.Client
}

// CatalogURLFromEnv is the base URL the environment selects.
func CatalogURLFromEnv() string {
	if v := os.Getenv(EnvCatalogURL); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultCatalogURL
}

// Lists implements [CatalogReader]: the index of catalog has an entry of
// chart at version. The index is fetched anew on every call, so a stale
// copy never answers.
func (c CatalogIndex) Lists(ctx context.Context, catalog, chart, version string) (bool, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultCatalogURL
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	url := fmt.Sprintf("%s/%s/index.yaml", strings.TrimRight(base, "/"), catalog)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return false, err
	}
	var index struct {
		Entries map[string][]struct {
			Version string `yaml:"version"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(data, &index); err != nil {
		return false, fmt.Errorf("parsing %s: %w", url, err)
	}
	for _, entry := range index.Entries[chart] {
		if entry.Version == version {
			return true, nil
		}
	}
	return false, nil
}
