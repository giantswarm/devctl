package ats

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// ConfigPath is the canonical, repository-owned ATS configuration file.
const ConfigPath = ".ats/main.yaml"

// HasConfig reports whether dir carries either configuration filename ATS
// accepts. A main.yml must not be shadowed by a generated main.yaml.
func HasConfig(dir string) (bool, error) {
	for _, name := range []string{ConfigPath, ".ats/main.yml"} {
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name))); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, microerror.Mask(err)
		}
	}
	return false, nil
}

// CreateConfig returns a starter configuration for a repository without ATS
// scenarios. The file is repository-owned after creation; unlike dependencies,
// it must never bypass the generator's regeneration check. ATS 1.x gets its
// cluster from the orb and no longer accepts the legacy per-scenario options.
func CreateConfig(uv bool) input.Input {
	return input.Input{
		Path: ConfigPath,
		TemplateBody: `# Created by devctl gen circleci. This file is repository-owned.
# Add ATS tests and remove their scenario names from skip-steps to enable them.
{{ if not . }}smoke-tests-cluster-type: kind
upgrade-tests-cluster-type: kind
{{ end }}skip-steps: [smoke, functional, upgrade]
`,
		TemplateData: uv,
	}
}
