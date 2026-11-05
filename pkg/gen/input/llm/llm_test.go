package llm_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
			require.Contains(t, body, "Where they differ from this block, they take precedence.")
			require.Contains(t, body, "- Use conventional commits for commit messages and pull request titles.\n")
			require.Equal(t, wantGo, strings.Contains(body, "goimports -local github.com/giantswarm/<repository>"))
			// Claude reads @ before a path or URL as an import.
			require.NotContains(t, body, " @")
			require.True(t, strings.HasSuffix(body, ".\n"), "the body ends with one newline")
		})
	}
}
