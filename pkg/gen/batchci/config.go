// Package batchci applies the repository-owned, staged batch-CI policy to
// generated workflows. Repositories without the policy retain byte-identical output.
package batchci

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const ConfigPath = ".devctl/ci-batch.yaml"
const GateName = "batch-ci"

// Result identifies both a check and its trusted producer. Actions checks also
// name their workflow, so a result from an older suite cannot mask a new run.
type Result struct {
	Name      string `yaml:"name" json:"name"`
	AppID     int64  `yaml:"appID,omitempty" json:"appID,omitempty"`
	Workflow  string `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	CreatorID int64  `yaml:"creatorID,omitempty" json:"creatorID,omitempty"`
}

type Config struct {
	Mode       string   `yaml:"mode"`
	BaseBranch string   `yaml:"baseBranch"`
	GateAppID  int64    `yaml:"gateAppID"`
	Results    []Result `yaml:"results"`
}

// Root locates the owning checkout even for gen precommit --dir outputs.
func Root(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, marker := range []string{".github/workflows/", ".circleci/"} {
		if i := strings.LastIndex(path, marker); i >= 0 {
			return filepath.FromSlash(path[:i])
		}
	}
	return "."
}

func Read(root string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(root, ConfigPath)) // #nosec G304 -- repository-owned generator configuration

	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Config, error) {
	var c Config
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigPath, err)
	}
	var extra interface{}
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s: expected exactly one policy document", ConfigPath)
	}
	if c.Mode != "prepare" && c.Mode != "batch" {
		return nil, fmt.Errorf("%s: mode must be prepare or batch", ConfigPath)
	}
	if c.BaseBranch == "" || c.GateAppID <= 0 || len(c.Results) == 0 {
		return nil, fmt.Errorf("%s: baseBranch, gateAppID and results are required", ConfigPath)
	}
	seen := map[string]bool{}
	for _, r := range c.Results {
		if r.AppID < 0 || r.CreatorID < 0 || (r.CreatorID > 0 && r.Workflow != "") || r.Name == "" || r.Name == GateName || seen[r.Name] || (r.AppID > 0) == (r.CreatorID > 0) || (r.AppID == c.GateAppID && r.Workflow == "") {
			return nil, fmt.Errorf("%s: each unique result needs a name and exactly one trusted appID (with workflow for Actions) or creatorID; the gate cannot require itself", ConfigPath)
		}
		seen[r.Name] = true
	}
	return &c, nil
}
