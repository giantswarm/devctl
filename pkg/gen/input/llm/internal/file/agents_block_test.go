package file

import (
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf16"

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

var (
	linkReferenceDefinition = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:\s`)
	notBareURL              = regexp.MustCompile("`[^`]*`|<https?://[^>]*>|\\]\\([^)]*\\)")
)

// Repositories lint AGENTS.md with markdownlint in pre-commit. The block passes
// its default line length (MD013: 80 UTF-16 code units, except a line without
// whitespace beyond it) and has no bare URLs (MD034), for every flavour and
// language.
func Test_AgentsBlock_PassesMarkdownlintDefaults(t *testing.T) {
	var cases []params.Params
	for _, language := range []string{"go", "generic"} {
		for _, f := range gen.AllFlavours() {
			cases = append(cases, params.Params{Flavours: gen.FlavourSlice{gen.Flavour(f)}, Language: language})
		}
		var all gen.FlavourSlice
		for _, f := range gen.AllFlavours() {
			all = append(all, gen.Flavour(f))
		}
		cases = append(cases, params.Params{Flavours: all, Language: language})
	}
	for _, p := range cases {
		body, err := AgentsBlockBody(p)
		require.NoError(t, err)
		merged, err := MergeManagedBlock(nil, body)
		require.NoError(t, err)
		for i, line := range strings.Split(string(merged), "\n") {
			units := utf16.Encode([]rune(line))
			if len(units) > 80 {
				beyond := string(utf16.Decode(units[80:]))
				require.False(t, strings.ContainsFunc(beyond, unicode.IsSpace),
					"%v %s: line %d is longer than 80 characters: %s", p.Flavours, p.Language, i+1, line)
			}
			if linkReferenceDefinition.MatchString(line) {
				continue
			}
			require.NotContains(t, notBareURL.ReplaceAllString(line, ""), "://",
				"%v %s: line %d has a bare URL: %s", p.Flavours, p.Language, i+1, line)
		}
	}
}
