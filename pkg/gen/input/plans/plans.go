package plans

import (
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/plans/internal/file"
)

func ClaudeSettings() input.Input {
	return file.NewClaudeSettingsInput(file.ClaudeSettingsPath)
}
