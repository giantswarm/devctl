package release

import "github.com/blang/semver"

type Requests struct {
	Releases []ReleaseRequest `yaml:"releases"`
}

type ReleaseRequest struct {
	Name     string    `yaml:"name"`
	Requests []Request `yaml:"requests"`
}

type Request struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

func (r Requests) ForVersion(version string) ([]Request, error) {
	var requests []Request

	v, err := semver.Parse(version)
	if err != nil {
		return nil, err
	}

	for _, releaseRequest := range r.Releases {
		constraint, err := semver.ParseRange(releaseRequest.Name)
		if err != nil {
			// Silently ignore failing constraints for now.
			continue
		}

		if constraint(v) {
			requests = append(requests, releaseRequest.Requests...)
		}
	}

	return requests, nil
}
