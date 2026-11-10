package file

import (
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/params"
)

// The block links to the template, so the template must stay where the link
// says.
func Test_agentsBlockSourceIsThisTemplate(t *testing.T) {
	require.True(t, strings.HasSuffix(agentsBlockSource, "/pkg/gen/input/llm/internal/file/"+agentsBlockTemplateName))
	_, err := os.Stat(path.Base(agentsBlockSource))
	require.NoError(t, err)
}

// Repositories lint AGENTS.md with markdownlint in pre-commit. The block passes
// its default line length (MD013: 80, except a line without whitespace beyond
// it) and has no bare URLs (MD034), so it lints clean with stricter settings
// than the repositories use.
func Test_AgentsBlock_PassesMarkdownlintDefaults(t *testing.T) {
	bareURL := regexp.MustCompile(`(^|[^<(])https?://`)
	for _, p := range []params.Params{
		{Flavours: gen.FlavourSlice{gen.FlavourGeneric}, Language: "generic"},
		{Flavours: gen.FlavourSlice{gen.FlavourApp}, Language: "go"},
		{Flavours: gen.FlavourSlice{gen.FlavourCLI}, Language: "go"},
	} {
		body, err := AgentsBlockBody(p)
		require.NoError(t, err)
		merged, err := MergeManagedBlock(nil, body)
		require.NoError(t, err)
		for i, line := range strings.Split(string(merged), "\n") {
			if len([]rune(line)) > 80 {
				require.NotContains(t, string([]rune(line)[80:]), " ", "line %d is longer than 80 characters: %s", i+1, line)
			}
			if !strings.HasPrefix(line, "[") {
				require.NotRegexp(t, bareURL, line, "line %d has a bare URL", i+1)
			}
		}
	}
}
