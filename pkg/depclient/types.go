package depclient

// Manifest type is copied and adjusted from
// https://github.com/golang/dep/blob/v0.5.0/manifest.go#L60-L84.
type Manifest struct {
	Constraints  []ManifestProject    `toml:"constraint,omitempty"`
	Overrides    []ManifestProject    `toml:"override,omitempty"`
	Ignored      []string             `toml:"ignored,omitempty"`
	Required     []string             `toml:"required,omitempty"`
	NoVerify     []string             `toml:"noverify,omitempty"`
	PruneOptions ManifestPruneOptions `toml:"prune,omitempty"`
}

type ManifestProject struct {
	Name     string `toml:"name"`
	Branch   string `toml:"branch,omitempty"`
	Revision string `toml:"revision,omitempty"`
	Version  string `toml:"version,omitempty"`
	Source   string `toml:"source,omitempty"`
}

type ManifestPruneOptions struct {
	UnusedPackages bool `toml:"unused-packages,omitempty"`
	NonGoFiles     bool `toml:"non-go,omitempty"`
	GoTests        bool `toml:"go-tests,omitempty"`

	//Projects []map[string]interface{} `toml:"project,omitempty"`
	Projects []map[string]interface{}
}
