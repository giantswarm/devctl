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
	err := agentsBlock.Execute(&b, map[string]any{"IsLanguageGo": params.IsLanguageGo(p)})
	if err != nil {
		return "", microerror.Mask(err)
	}
	return b.String(), nil
}
