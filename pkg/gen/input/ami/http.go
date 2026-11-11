package ami

import (
	"context"
	"net/http"

	"github.com/giantswarm/microerror"
)

// fetchReleaseVersions lists the releases in a release directory. The
// response body is closed before it returns, on every path.
func fetchReleaseVersions(ctx context.Context, url string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	response, err := http.DefaultClient.Do(request) //nolint:gosec
	if err != nil {
		return nil, microerror.Mask(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, microerror.Maskf(executionFailedError, "fetching %s: HTTP status %d", url, response.StatusCode)
	}
	return scrapeVersions(response.Body)
}

// fetchReleaseAMI fetches a release's AMIs. It reports false when the release
// is not published (HTTP 403). The response body is closed before it returns,
// on every path, so a loop over releases holds at most one open response.
func fetchReleaseAMI(ctx context.Context, url string) (map[string]string, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, microerror.Mask(err)
	}
	response, err := http.DefaultClient.Do(request) //nolint:gosec
	if err != nil {
		return nil, false, microerror.Mask(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusForbidden {
		return nil, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, microerror.Maskf(executionFailedError, "fetching %s: HTTP status %d", url, response.StatusCode)
	}
	versionAMI, err := scrapeVersionAMI(response.Body)
	if err != nil {
		return nil, false, microerror.Mask(err)
	}
	return versionAMI, true, nil
}
