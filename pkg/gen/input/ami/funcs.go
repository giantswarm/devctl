package ami

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"golang.org/x/net/html"
)

func getAMIInfoString(config Config) (string, error) {
	var versions []string
	{
		url := fmt.Sprintf("https://%s.release.%s/%s/", config.Channel, config.PrimaryDomain, config.Arch)
		fmt.Println("scraping", url)
		response, err := http.Get(url) //nolint:gosec
		if err != nil {
			return "", microerror.Mask(err)
		}

		versions, err = scrapeVersions(response.Body)
		if err != nil {
			return "", microerror.Mask(err)
		}
	}

	mergedAMI := map[string]map[string]string{}
	for _, version := range versions {
		if semver.MustParse(version).LessThan(semver.MustParse(config.MinimumVersion)) {
			continue
		}
		url := fmt.Sprintf("https://%s.release.%s/%s/%s/flatcar_production_ami_all.json", config.Channel, config.PrimaryDomain, config.Arch, version)
		fmt.Println("scraping", url)
		response, err := http.Get(url) //nolint:gosec
		if err != nil {
			return "", microerror.Mask(err)
		}
		if response.StatusCode == 403 {
			continue // not found, keep going
		}
		mergedAMI[version], err = scrapeVersionAMI(response.Body)
		if err != nil {
			return "", microerror.Mask(err)
		}
	}

	for version := range mergedAMI {
		url := fmt.Sprintf("https://%s/%s/%s/%s.json", config.ChinaDomain, config.Channel, config.Arch, version)
		fmt.Println("scraping", url)
		response, err := http.Get(url) //nolint:gosec
		if err != nil {
			return "", microerror.Mask(err)
		}
		if response.StatusCode == 403 {
			continue
		}
		chinaVersionAMI, err := scrapeVersionAMI(response.Body)
		if err != nil {
			return "", microerror.Mask(err)
		}
		for region, image := range chinaVersionAMI {
			mergedAMI[version][region] = image
		}
	}

	result, err := json.MarshalIndent(mergedAMI, "", "  ")
	if err != nil {
		return "", microerror.Mask(err)
	}

	return fmt.Sprintf("`%s`", result), nil
}

func scrapeVersions(source io.Reader) ([]string, error) {
	z := html.NewTokenizer(source)
	var versions []string
	for {
		tt := z.Next()
		switch {
		case tt == html.ErrorToken:
			return versions, nil
		case tt == html.StartTagToken:
			t := z.Token()
			if t.Data != "a" {
				continue
			}
			for _, attr := range t.Attr {
				if attr.Key != "href" {
					continue
				}
				// Versions to extract look like href="./123.4.5/" or href="123.4.5"
				// so we trim off suffix and prefix if they exist and then ensure this
				// is a valid semver version.
				href := strings.TrimPrefix(strings.TrimSuffix(attr.Val, "/"), "./")
				if _, err := semver.NewVersion(href); err != nil {
					break // href is invalid, no need to look at other attrs
				}
				versions = append(versions, href)
			}
		}
	}
}

func scrapeVersionAMI(source io.Reader) (map[string]string, error) {
	body, err := io.ReadAll(source)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	type amiInfo struct {
		Name string `json:"name"`
		PV   string `json:"pv"`
		HVM  string `json:"hvm"`
	}

	type amiInfoList struct {
		AMI []amiInfo `json:"amis"`
	}

	var list amiInfoList
	err = json.Unmarshal(body, &list)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	result := map[string]string{}
	for _, region := range list.AMI {
		result[region.Name] = region.HVM
	}

	return result, nil
}
