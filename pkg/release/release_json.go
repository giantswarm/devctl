package release

type ReleaseJsonInfo struct {
	Version          string `json:"version"`
	IsDeprecated     bool   `json:"isDeprecated"`
	ReleaseTimestamp string `json:"releaseTimestamp"`
	ChangelogUrl     string `json:"changelogUrl"`
	IsStable         bool   `json:"isStable"`
}

type ReleasesJsonData struct {
	Releases     []ReleaseJsonInfo `json:"releases"`
	SourceUrl    string            `json:"sourceUrl"`
	ChangelogUrl string            `json:"changelogUrl"`
	Homepage     string            `json:"homepage"`
}
