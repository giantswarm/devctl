package llm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm"
)

func TestBlockBody(t *testing.T) {
	for language, wantGo := range map[string]bool{"go": true, "generic": false, "": false} {
		t.Run(language, func(t *testing.T) {
			l, err := llm.New(llm.Config{Language: language})
			require.NoError(t, err)

			body, err := l.BlockBody()
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(body, "## Giant Swarm conventions\n"))
			text := strings.Join(strings.Fields(body), " ")
			require.Contains(t, text, "Where they differ from this block, they take precedence.")
			require.Contains(t, text, "- Use conventional commits for commit messages and pull request titles.")
			require.Equal(t, wantGo, strings.Contains(text, "goimports -local github.com/giantswarm/<repository>"))
			// Claude reads @ before a path or URL as an import.
			require.NotContains(t, body, " @")
			require.True(t, strings.HasSuffix(body, ".\n"), "the body ends with one newline")
		})
	}
}

func TestBlockBody_RecommendsPluginsForTheRepository(t *testing.T) {
	const line = "- If the skills of the `giantswarm/claude-code` plugin marketplace are not available to you and the user works at Giant Swarm, tell them once that they can install the %s plugins from it."
	for _, tc := range []struct {
		name   string
		config llm.Config
		want   string
	}{
		{name: "generic", config: llm.Config{Language: "generic", Flavours: gen.FlavourSlice{gen.FlavourGeneric}}, want: "`gs-base` and `gs-dev`"},
		{name: "Go", config: llm.Config{Language: "go", Flavours: gen.FlavourSlice{gen.FlavourCLI}}, want: "`gs-base`, `gs-dev` and `gs-godev`"},
		{name: "app", config: llm.Config{Language: "generic", Flavours: gen.FlavourSlice{gen.FlavourApp}}, want: "`gs-base`, `gs-dev` and `gs-appdev`"},
		{name: "Go app", config: llm.Config{Language: "go", Flavours: gen.FlavourSlice{gen.FlavourApp}}, want: "`gs-base`, `gs-dev`, `gs-godev` and `gs-appdev`"},
		{name: "cluster app", config: llm.Config{Language: "generic", Flavours: gen.FlavourSlice{gen.FlavourClusterApp}}, want: "`gs-base` and `gs-dev`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, err := llm.New(tc.config)
			require.NoError(t, err)

			body, err := l.BlockBody()
			require.NoError(t, err)
			require.Contains(t, strings.Join(strings.Fields(body), " "), fmt.Sprintf(line, tc.want))
		})
	}
}
