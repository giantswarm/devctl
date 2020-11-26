package file

import (
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/dependabot/internal/params"
	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func NewCreateWorkflowInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, "workflows", internal.RegenerableFilePrefix+"gomodtidy.yaml"),
		TemplateBody: createDependabotWorkflowTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}

var createDependabotWorkflowTemplate = `{{{{ .Header }}}}
# Credit: https://github.com/crazy-max/diun
name: auto-go-mod-tidy

on:
  push:
    branches:
      - 'dependabot/**'

jobs:
  fix:
    runs-on: ubuntu-latest
    steps:
      -
        name: Checkout
        uses: actions/checkout@v1
      -
        # https://github.com/actions/checkout/issues/6
        name: Fix detached HEAD
        run: git checkout ${GITHUB_REF#refs/heads/}
      -
        name: Tidy
        run: |
          rm -f go.sum
          go mod tidy
      -
        name: Set up Git
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          git config user.name "${GITHUB_ACTOR}"
          git config user.email "${GITHUB_ACTOR}@users.noreply.github.com"
          git remote set-url origin https://x-access-token:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git
      -
        name: Commit and push changes
        run: |
          git add .
          if output=$(git status --porcelain) && [ ! -z "$output" ]; then
            git commit -m 'Fix go modules'
            git push
          fi
`
