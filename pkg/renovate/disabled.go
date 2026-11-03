package renovate

import "regexp"

// topLevelDisabled matches a top-level `enabled: false` of a configuration,
// JSON or JSON5; one nested in packageRules is indented further.
var topLevelDisabled = regexp.MustCompile(`(?m)^\s{0,2}["']?enabled["']?\s*:\s*false`)

// Disables says whether a Renovate configuration turns Renovate off for the
// whole repository.
func Disables(config []byte) bool {
	return topLevelDisabled.Match(config)
}
