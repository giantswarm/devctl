package changelog

import (
	"encoding/json"

	"github.com/giantswarm/microerror"
)

type flatcarRelease struct {
	ReleaseNotes string `json:"release_notes"`
}

func parseFlatcarChangelog(body []byte, componentVersion string) (string, error) {
	var releases map[string]flatcarRelease
	err := json.Unmarshal(body, &releases)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return releases[componentVersion].ReleaseNotes, nil
}
