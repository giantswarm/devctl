package ami

import (
	"net/http"

	"github.com/giantswarm/microerror"
)

func fetchReleaseVersions(url string) ([]string, error) {
	response, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, microerror.Mask(err)
	}
	defer func() { _ = response.Body.Close() }()

	return scrapeVersions(response.Body)
}

func fetchReleaseAMI(url string) (map[string]string, error) {
	response, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, microerror.Mask(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusForbidden {
		return nil, nil
	}
	return scrapeVersionAMI(response.Body)
}
