package changelog

import (
	"encoding/json"
	"fmt"

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

	b, ok := releases[componentVersion]
	if !ok {
		return fmt.Sprintf("Containerlinux release %q was not found in the changelog", componentVersion), nil
	}

	var release containerlinuxRelease
	err = json.Unmarshal(b, &release)
	if err != nil {
		return "", microerror.Mask(err)
	}
	return release.ReleaseNotes, nil
}
