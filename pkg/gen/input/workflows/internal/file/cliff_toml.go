package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed cliff.toml.template
var cliffTemplate string

//go:generate go run ../../../update-template-sha.go cliff.toml.template
//go:embed cliff.toml.template.sha
var cliffTemplateSha string

// NewCliffInput returns the Input for cliff.toml in the repository root. It is
// the git-cliff config the push-based auto-release workflow uses to compute the
// next version and render release notes. The repo name is templated into the
// [remote.github] section so git-cliff resolves PR links and authors against
// the consuming repo.
func NewCliffInput(repoName string) input.Input {
	return input.Input{
		Path:         filepath.Join(".", "cliff.toml"),
		TemplateBody: cliffTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":   params.Header("#", cliffTemplateSha),
			"RepoName": repoName,
		},
	}
}
