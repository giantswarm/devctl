package llm_test

import (
	"fmt"
	"slices"
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
			require.Contains(t, strings.Join(strings.Fields(body), " "), "Where they differ from this block, they take precedence.")
			items := listItems(body)
			require.Contains(t, items, "Use conventional commits for commit messages and pull request titles.")
			require.Equal(t, wantGo, slices.ContainsFunc(items, func(item string) bool {
				return strings.HasPrefix(item, "Go: run `goimports -local github.com/giantswarm/<repository>`")
			}))
			// Claude reads @ before a path or URL as an import.
			require.NotContains(t, body, " @")
			require.True(t, strings.HasSuffix(body, ".\n"), "the body ends with one newline")
		})
	}
}

func TestBlockBody_SpecificationRules(t *testing.T) {
	const (
		specItem  = "<https://github.com/giantswarm/fmt/blob/main/design/specifications.md>"
		namesItem = "<https://github.com/giantswarm/fmt/blob/main/design/names.md>"
	)
	for _, tc := range []struct {
		name     string
		config   llm.Config
		wantSpec bool
	}{
		{name: "Go", config: llm.Config{Language: "go", Flavours: gen.FlavourSlice{gen.FlavourCLI}}, wantSpec: true},
		{name: "node", config: llm.Config{Language: "node", Flavours: gen.FlavourSlice{gen.FlavourApp}}, wantSpec: true},
		{name: "python", config: llm.Config{Language: "python", Flavours: gen.FlavourSlice{gen.FlavourGeneric}}, wantSpec: true},
		{name: "Kubernetes API types", config: llm.Config{Language: "generic", Flavours: gen.FlavourSlice{gen.FlavourKubernetesAPI}}, wantSpec: true},
		{name: "chart", config: llm.Config{Language: "generic", Flavours: gen.FlavourSlice{gen.FlavourApp}}},
		{name: "customer", config: llm.Config{Language: "generic", Flavours: gen.FlavourSlice{gen.FlavourCustomer}}},
		{name: "kyverno policy", config: llm.Config{Language: "kyverno-policy", Flavours: gen.FlavourSlice{gen.FlavourGeneric}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, err := llm.New(tc.config)
			require.NoError(t, err)

			body, err := l.BlockBody()
			require.NoError(t, err)

			items := strings.Join(listItems(body), "\n")
			require.Equal(t, tc.wantSpec, strings.Contains(items, specItem))
			require.Equal(t, tc.wantSpec, strings.Contains(items, "open a draft pull request when nobody can be asked"))
			require.Contains(t, items, "keep one meaning per word")
			require.Contains(t, items, namesItem)
		})
	}
}

func TestBlockBody_RecommendsPluginsForTheRepository(t *testing.T) {
	const item = "If the skills of the `giantswarm/claude-code` plugin marketplace are not available to you and the user works at Giant Swarm, tell them once that they can install the %s plugins from it."
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
			require.Contains(t, listItems(body), fmt.Sprintf(item, tc.want))
		})
	}
}

// listItems returns the text of each top-level list item in body, with its
// lines joined by single spaces.
func listItems(body string) []string {
	var items []string
	inItem := false
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			items = append(items, strings.TrimPrefix(line, "- "))
			inItem = true
		case inItem && strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "":
			items[len(items)-1] += " " + strings.TrimSpace(line)
		default:
			inItem = false
		}
	}
	return items
}
