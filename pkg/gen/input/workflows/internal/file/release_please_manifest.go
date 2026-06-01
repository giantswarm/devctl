package file

import (
	_ "embed"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
)

//go:embed release_please_manifest.json.template
var releasePleaseManifestTemplate string

// semverTagRegexp matches the GiantSwarm "v<major>.<minor>.<patch>" tag
// convention used by the legacy create_release flow. Pre-release suffixes are
// excluded on purpose: they should not become the manifest baseline.
var semverTagRegexp = regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`)

// latestReleasedVersion returns the highest "v<major>.<minor>.<patch>" tag in
// the working directory (with the leading "v" stripped), or "" if there are no
// such tags or git is not available. The cwd at gen time is the consumer repo.
func latestReleasedVersion() string {
	// --sort=-v:refname orders tags by version descending, so the first match
	// is the highest.
	cmd := exec.Command("git", "tag", "--list", "v*.*.*", "--sort=-v:refname")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if m := semverTagRegexp.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1]
		}
	}
	return ""
}

// NewReleasePleaseManifestInput returns a scaffolding Input (generate-once) for
// .release-please-manifest.json in the repository root. Release Please updates
// this file on every run to track the current released version.
//
// For a green-field repo with no prior tags the manifest is initialized empty
// ({}); release-please will populate it on the first release. For a repo
// migrating from the legacy create_release flow that already has v*.*.* tags
// (root component), the manifest is seeded with {".": "<latest>"} — without
// this seed release-please emits
//
//	Found release tag with component '', but not configured in manifest
//
// and exits without opening a Release PR because it has no per-path baseline
// to compute "what changed since the last release" against.
func NewReleasePleaseManifestInput() input.Input {
	body := releasePleaseManifestTemplate
	if v := latestReleasedVersion(); v != "" {
		body = fmt.Sprintf("{\n  \".\": \"%s\"\n}\n", v)
	}
	return input.Input{
		Path:         filepath.Join(".", ".release-please-manifest.json"),
		TemplateBody: body,
	}
}
