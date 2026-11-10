package file

import (
	_ "embed"
	"strings"
	"text/template"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/params"
)

const agentsBlockTemplateName = "agents_block.md.template"

//go:embed agents_block.md.template
var agentsBlockTemplate string

var agentsBlock = template.Must(template.New("agents_block").Parse(agentsBlockTemplate))

// AgentsBlockBody renders the content of the devctl-managed block in
// AGENTS.md.
func AgentsBlockBody(p params.Params) (string, error) {
	var b strings.Builder
	err := agentsBlock.Execute(&b, map[string]any{
		"IsLanguageGo": params.IsLanguageGo(p),
		"Plugins":      plugins(p),
		"Source":       agentsBlockSource,
	})
	if err != nil {
		return "", microerror.Mask(err)
	}
	return b.String(), nil
}

// plugins lists the plugins of the giantswarm/claude-code marketplace for
// the repository, as prose.
func plugins(p params.Params) string {
	names := []string{"gs-base", "gs-dev"}
	if params.IsLanguageGo(p) {
		names = append(names, "gs-godev")
	}
	if params.IsFlavourApp(p) {
		names = append(names, "gs-appdev")
	}
	for i, name := range names {
		names[i] = "`" + name + "`"
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " and " + names[last]
}
