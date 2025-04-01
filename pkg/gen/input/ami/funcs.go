package ami

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"golang.org/x/net/html"
)

func getAMIInfoString(config Config) (string, error) {
	existing := map[string]map[string]string{}
	if config.KeepExisting != "" {
		// Read versions already defined in file.
		data, err := os.ReadFile(config.KeepExisting)
		if err != nil {
			return "", microerror.Mask(err)
		}
		err = json.Unmarshal(data, &existing)
		if err != nil {
			return "", microerror.Mask(err)
		}
	}

	var versions []string
	{
		url := fmt.Sprintf("https://%s.release.%s/%s/", config.Channel, config.PrimaryDomain, config.Arch)
		fmt.Println("getting list of releases from", url)
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
		if val, found := existing[version]; found {
			fmt.Printf("Release %s already present in %s, not scraping it\n", version, config.KeepExisting)
			mergedAMI[version] = val
			delete(existing, version)
			continue
		}

		url := fmt.Sprintf("https://%s.release.%s/%s/%s/flatcar_production_ami_all.json", config.Channel, config.PrimaryDomain, config.Arch, version)
		fmt.Println("scraping release", version)
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

		chinaVersionAMI, err := getChinaFlatcarRelease(config, version)
		if err != nil {
			return "", microerror.Mask(err)
		}

		for region, image := range chinaVersionAMI {
			mergedAMI[version][region] = image
		}
	}

	// Releases defined in the existing file but not scraped successfully for some reason.
	for version, val := range existing {
		mergedAMI[version] = val
	}

	result, err := json.MarshalIndent(mergedAMI, "", "  ")
	if err != nil {
		return "", microerror.Mask(err)
	}

	return string(result), nil
}

func scrapeVersions(source io.Reader) ([]string, error) {
	z := html.NewTokenizer(source)
	var versions []string
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return versions, nil
		case html.StartTagToken:
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
