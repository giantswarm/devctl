package releasewait

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

// architectOrb is the orb whose push-to-app-catalog job publishes charts.
const architectOrb = "giantswarm/architect"

// prereleasesToTestFrom is the first architect orb release whose
// push-to-app-catalog sends the chart of a pre-release tag (v1.2.3-rc.1) to
// app_catalog_test instead of app_catalog.
var prereleasesToTestFrom = semver.MustParse("10.12.0")

// ArchitectOrbPin is the architect orb version the tag's CircleCI
// configuration pins, e.g. "10.12.0" or "dev:my-branch": custom.yml's pin
// over workflows.yml's over config.yml's, as custom.yml deep-merges over the
// generated workflows. Empty when no file pins the orb.
func ArchitectOrbPin(ctx context.Context, gh GitHub, owner, repo, sha string, content TagContent) (string, error) {
	for _, name := range []string{circleCICustom, circleCIWorkflows, circleCIConfig} {
		if !slices.Contains(content.CircleCI, name) {
			continue
		}
		file, err := gh.GetFile(ctx, owner, repo, circleCIDir+"/"+name, sha)
		if err != nil {
			return "", fmt.Errorf("reading %s/%s at %s: %w", circleCIDir, name, short(sha), err)
		}
		pin, err := parseArchitectOrbPin(file.Data)
		if err != nil {
			return "", usageErr("%s/%s at %s: %v", circleCIDir, name, short(sha), err)
		}
		if pin != "" {
			return pin, nil
		}
	}
	return "", nil
}

// parseArchitectOrbPin reads the version of the architect orb from a
// CircleCI configuration's orbs, whatever alias it is imported under.
func parseArchitectOrbPin(config []byte) (string, error) {
	var doc struct {
		Orbs map[string]any `yaml:"orbs"`
	}
	if err := yaml.Unmarshal(config, &doc); err != nil {
		return "", fmt.Errorf("parsing the CircleCI configuration: %w", err)
	}
	for _, ref := range doc.Orbs {
		s, ok := ref.(string)
		if !ok {
			continue
		}
		if name, version, found := strings.Cut(s, "@"); found && name == architectOrb {
			return version, nil
		}
	}
	return "", nil
}

// PrereleasesToTestCatalog says whether an architect orb pin sends a
// pre-release tag's chart to the test catalog: a release from 10.12.0 on, or
// a dev version, which is published from a branch of the orb's current main.
func PrereleasesToTestCatalog(pin string) bool {
	if strings.HasPrefix(pin, "dev:") {
		return true
	}
	v, err := semver.StrictNewVersion(pin)
	if err != nil {
		return false
	}
	return !v.LessThan(prereleasesToTestFrom)
}

// isPrerelease says whether a version (without the v) carries a pre-release.
func isPrerelease(version string) bool {
	v, err := semver.NewVersion(version)
	return err == nil && v.Prerelease() != ""
}
