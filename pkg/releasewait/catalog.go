package releasewait

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"
	"gopkg.in/yaml.v3"
)

// EnvCatalogURL points --catalog at another host for the catalog indexes;
// the default is the GitHub Pages host of the catalogs.
const EnvCatalogURL = "DEVCTL_CATALOG_URL"

// DefaultCatalogURL is where the catalog indexes are published:
// <DefaultCatalogURL>/<catalog>/index.yaml.
const DefaultCatalogURL = "https://giantswarm.github.io"

// CatalogPublishGrace is the single extension granted after the normal wait
// expires when every chart still missing from Pages is in its repository.
const CatalogPublishGrace = 10 * time.Minute

// CatalogRepositoryReader optionally supplies repository publication evidence
// beside a CatalogReader's Pages index. Repository evidence alone is not success.
type CatalogRepositoryReader interface {
	RepositoryLists(ctx context.Context, catalog, chart, version string) (bool, error)
}

// CatalogIndex reads a catalog's index.yaml over HTTP.
type CatalogIndex struct {
	// BaseURL is the host of the indexes; empty means DefaultCatalogURL.
	BaseURL string
	// HTTPClient sends the requests; nil means the default client.
	HTTPClient *http.Client
	// GitHub reads the catalog repository's default-branch index, when set.
	GitHub *github.Client
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
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return false, err
	}
	return catalogIndexLists(data, chart, version)
}

// RepositoryLists checks the catalog repository, independently of Pages. The
// raw media type also reads indexes larger than the Contents API's 1 MB JSON
// content limit. No ref selects the repository's default branch.
func (c CatalogIndex) RepositoryLists(ctx context.Context, catalog, chart, version string) (bool, error) {
	if c.GitHub == nil {
		return false, nil
	}
	req, err := c.GitHub.NewRequest(ctx, http.MethodGet, fmt.Sprintf("repos/giantswarm/%s/contents/index.yaml", catalog), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("Cache-Control", "no-cache")
	var data bytes.Buffer
	if _, err := c.GitHub.Do(req, &data); err != nil {
		var response *github.ErrorResponse
		if errors.As(err, &response) && response.Response.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return catalogIndexLists(data.Bytes(), chart, version)
}

func catalogIndexLists(data []byte, chart, version string) (bool, error) {
	var index struct {
		Entries map[string][]struct {
			Version string `yaml:"version"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(data, &index); err != nil {
		return false, fmt.Errorf("parsing catalog index: %w", err)
	}
	for _, entry := range index.Entries[chart] {
		if entry.Version == version {
			return true, nil
		}
	}
	return false, nil
}

// catalogLists checks Pages first and records repository publication as
// progress. Confirmed repository entries need no further API reads.
func (w *Waiter) catalogLists(ctx context.Context, result *Result, p *plan) (bool, error) {
	if p.catalogs == nil {
		catalogs, err := w.chartCatalogs(ctx, result, p)
		if err != nil {
			return false, err
		}
		p.catalogs = catalogs
		p.catalogConfirmed = map[int]bool{}
	}
	pending, ready, err := w.catalogPending(ctx, p, p.catalogConfirmed)
	if err != nil {
		return false, err
	}
	w.catalogWaiting, p.catalogReady = pending, len(pending) > 0 && ready
	for _, message := range pending {
		w.progress.Printf("%s", message)
	}
	return len(pending) == 0, nil
}

func (w *Waiter) catalogPending(ctx context.Context, p *plan, confirmed map[int]bool) ([]string, bool, error) {
	var pending []string
	ready := true
	for i, chart := range p.charts {
		catalog := p.catalogs[i]
		listed, err := w.config.CatalogIndex.Lists(ctx, catalog, chart.name, p.version)
		if err != nil {
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			return nil, false, usageErr("reading the %s Pages index: %v", catalog, err)
		}
		if listed {
			continue
		}
		message := fmt.Sprintf("%s/%s %s", catalog, chart.name, p.version)
		if reader, ok := w.config.CatalogIndex.(CatalogRepositoryReader); ok && !confirmed[i] {
			confirmed[i], err = reader.RepositoryLists(ctx, catalog, chart.name, p.version)
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
		}
		switch {
		case confirmed[i]:
			message += " in repository index.yaml, Pages not yet serving"
		case err != nil:
			message += fmt.Sprintf(" not served by Pages; repository index could not be checked: %v", err)
			ready = false
		default:
			message += " not served by Pages; no repository index entry confirmed"
			ready = false
		}
		pending = append(pending, message)
	}
	return pending, ready, nil
}
