package ami

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/giantswarm/microerror"
	"golang.org/x/net/html"

	"github.com/giantswarm/devctl/pkg/gen/input"
)

var amiTemplate = `// NOTE: This file is generated. Do not edit.
package {{ .Package }}

import "encoding/json"

var amiJSON = []byte({{ .AMIInfoString }})
var amiInfo = map[string]map[string]string{}

func init() {
	err := json.Unmarshal(amiJSON, &amiInfo)
	if err != nil {
		panic(err)
	}
}
`

type AMI struct {
	arch           string
	channel        string
	chinaDomain    string
	dir            string
	minimumVersion string
	primaryDomain  string
}

func NewAMI(config Config) (*AMI, error) {
	err := config.Validate()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	f := &AMI{
		arch:           config.Arch,
		channel:        config.Channel,
		chinaDomain:    config.ChinaDomain,
		dir:            config.Dir,
		minimumVersion: config.MinimumVersion,
		primaryDomain:  config.PrimaryDomain,
	}

	return f, nil
}

type sourceFileTemplateData struct {
	AMIInfoString string
	Package       string
}

type amiInfo struct {
	Name string `json:"name"`
	PV   string `json:"pv"`
	HVM  string `json:"hvm"`
}

type amiInfoList struct {
	AMI []amiInfo `json:"amis"`
}

func (f *AMI) GetInput(ctx context.Context) (input.Input, error) {
	templateData, err := f.getAMI(ctx)
	if err != nil {
		return input.Input{}, microerror.Mask(err)
	}
	i := input.Input{
		Path:         filepath.Join(f.dir, "ami.go"),
		TemplateBody: amiTemplate,
		TemplateData: templateData,
	}

	return i, nil
}

func (f *AMI) getAMI(ctx context.Context) (map[string]interface{}, error) {
	var versions []string
	{
		url := fmt.Sprintf("https://%s.release.%s/%s/", f.channel, f.primaryDomain, f.arch)
		fmt.Println("scraping", url)
		response, err := http.Get(url) //nolint:gosec
		if err != nil {
			return nil, microerror.Mask(err)
		}

		versions, err = scrapeVersions(response.Body)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	mergedAMI := map[string]map[string]string{}
	for _, version := range versions {
		if semver.MustParse(version).LessThan(semver.MustParse(f.minimumVersion)) {
			continue
		}
		url := fmt.Sprintf("https://%s.release.%s/%s/%s/flatcar_production_ami_all.json", f.channel, f.primaryDomain, f.arch, version)
		fmt.Println("scraping", url)
		response, err := http.Get(url) //nolint:gosec
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if response.StatusCode == 403 {
			continue // not found, keep going
		}
		mergedAMI[version], err = scrapeVersionAMI(response.Body)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	for version := range mergedAMI {
		url := fmt.Sprintf("https://%s/%s/%s/%s.json", f.chinaDomain, f.channel, f.arch, version)
		fmt.Println("scraping", url)
		response, err := http.Get(url) //nolint:gosec
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if response.StatusCode == 403 {
			continue
		}
		chinaVersionAMI, err := scrapeVersionAMI(response.Body)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for region, image := range chinaVersionAMI {
			mergedAMI[version][region] = image
		}
	}

	result, err := json.MarshalIndent(mergedAMI, "", "  ")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	templateData := map[string]interface{}{
		"AMIInfoString": fmt.Sprintf("`%s`", result),
		"Package":       "key",
	}

	return templateData, nil
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
				href := strings.TrimSuffix(attr.Val, "/")
				if strings.HasPrefix(href, "./") {
					href = strings.TrimPrefix(href, "./")
				}
				if _, err := semver.NewVersion(href); err != nil {
					break // href is invalid, no need to look at other attrs
				}
				versions = append(versions, href)
			}
		}
	}
}

func scrapeVersionAMI(source io.Reader) (map[string]string, error) {
	body, err := ioutil.ReadAll(source)
	if err != nil {
		return nil, microerror.Mask(err)
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
