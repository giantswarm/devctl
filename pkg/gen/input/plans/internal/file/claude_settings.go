package file

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

const (
	ClaudeSettingsPath = ".claude/settings.json"

	marketplaceName = "giantswarm"
	marketplaceRepo = "giantswarm/claude-code"
	plansPlugin     = "gs-plans@" + marketplaceName
)

// NewClaudeSettingsInput enables the gs-plans plugin from the Giant Swarm
// marketplace in path. Only the marketplace entry and the plugin's entry are
// owned by devctl: every other key of an existing file is kept, so a team can
// add its own settings next to them.
func NewClaudeSettingsInput(path string) input.Input {
	return input.Input{
		Path:           path,
		SkipRegenCheck: true,
		Generate: func(ctx context.Context) ([]byte, error) {
			existing, err := os.ReadFile(path) // #nosec G304 -- fixed path inside the repository being generated
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, microerror.Mask(err)
			}
			return mergeClaudeSettings(existing)
		},
	}
}

func mergeClaudeSettings(existing []byte) ([]byte, error) {
	settings := map[string]any{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &settings); err != nil {
			return nil, microerror.Maskf(invalidSettingsError, "%s is not a JSON object: %v", ClaudeSettingsPath, err)
		}
	}

	marketplaces := objectAt(settings, "extraKnownMarketplaces")
	marketplaces[marketplaceName] = map[string]any{
		"source": map[string]any{
			"source": "github",
			"repo":   marketplaceRepo,
		},
	}

	plugins := objectAt(settings, "enabledPlugins")
	plugins[plansPlugin] = true

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return append(out, '\n'), nil
}

// objectAt returns settings[key] as an object, replacing a missing or
// non-object value with an empty one.
func objectAt(settings map[string]any, key string) map[string]any {
	if m, ok := settings[key].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	settings[key] = m
	return m
}
