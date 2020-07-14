package changelog

import (
	"encoding/json"

	"github.com/giantswarm/microerror"
)

type containerlinuxRelease struct {
	ReleaseNotes string `json:"release_notes"`
}

func parseContainerLinuxChangelog(body []byte, componentVersion string) (string, error) {
	var releases map[string]json.RawMessage
	err := json.Unmarshal(body, &releases)
	if err != nil {
		return "", microerror.Mask(err)
	}
	var release containerlinuxRelease
	err = json.Unmarshal(releases[componentVersion], &release)
	if err != nil {
		return "", microerror.Mask(err)
	}
	return release.ReleaseNotes, nil
}
