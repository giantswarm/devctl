package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.go.mk.template
var makefileGenGoMkTemplate string

//go:generate go run ../../../update-template-sha.go Makefile.gen.go.mk.template
//go:embed Makefile.gen.go.mk.template*
var makefileGenGoMkTemplateFiles embed.FS

var makefileGenGoMkTemplateSha = input.TemplateSHA(makefileGenGoMkTemplateFiles, "Makefile.gen.go.mk.template")

//go:embed windows-code-signing.sh.template
var windowsCodeSigningShellScriptTemplate string

//go:generate go run ../../../update-template-sha.go windows-code-signing.sh.template
//go:embed windows-code-signing.sh.template*
var windowsCodeSigningShellScriptTemplateFiles embed.FS

var windowsCodeSigningShellScriptTemplateSha = input.TemplateSHA(windowsCodeSigningShellScriptTemplateFiles, "windows-code-signing.sh.template")

func NewMakefileGenGoMkInput(p params.Params) []input.Input {
	inputs := []input.Input{
		{
			Path:         "Makefile.gen.go.mk",
			TemplateBody: makefileGenGoMkTemplate,
			TemplateData: map[string]interface{}{
				"IsFlavourCLI":    params.IsFlavourCLI(p),
				templateKeyHeader: params.Header("#", makefileGenGoMkTemplateSha),
			},
		},
	}

	if params.IsFlavourCLI(p) {
		inputs = append(inputs, input.Input{
			Path:         ".github/zz_generated.windows-code-signing.sh",
			Permissions:  0755,
			TemplateBody: windowsCodeSigningShellScriptTemplate,
			TemplateData: map[string]interface{}{
				templateKeyHeader: params.Header("#", windowsCodeSigningShellScriptTemplateSha),
			},
		})
	}

	return inputs
}
