package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

func NewCreateReleasePRInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create_release_pr.yaml"),
		TemplateBody: createReleasePRTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
	}

	return i
}

var createReleasePRTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen workflows
#
name: Create Release PR
on:
  push:
    branches:
      - 'master#release#v*.*.*'
      - 'legacy#release#v*.*.*'
      - 'release-v*.*.x#release#v*.*.*'
      # "!" negates previous positive patterns so it has to be at the end.
      - '!release-v*.x.x#release#v*.*.*'
jobs:
  debug_info:
    name: Debug info
    runs-on: ubuntu-20.04
    steps:
      - name: Print github context JSON
        run: |
          cat <<EOF
          ${{ toJson(github) }}
          EOF
  gather_facts:
    name: Gather facts
    runs-on: ubuntu-20.04
    outputs:
      base: ${{ steps.gather_facts.outputs.base }}
      skip: ${{ steps.pr_exists.outputs.skip }}
      version: ${{ steps.gather_facts.outputs.version }}
    steps:
      - name: Gather facts
        id: gather_facts
        run: |
          head="${{ github.event.ref }}"
          head="${head#refs/heads/}" # Strip "refs/heads/" prefix.
          base="$(echo $head | cut -d '#' -f 1)"
          base="${base#refs/heads/}" # Strip "refs/heads/" prefix.
          version="$(echo $head | cut -d '#' -f 3)"
          version="${version#v}" # Strip "v" prefix.
          echo "base=\"$base\" head=\"$head\" version=\"$version\""
          echo "::set-output name=base::${base}"
          echo "::set-output name=head::${head}"
          echo "::set-output name=version::${version}"
      - name: Check if PR exists
        id: pr_exists
        env:
          GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
        run: |
          if gh pr view --repo ${{ github.repository }} ${{ github.event.ref }} ; then
            echo "::warning::Release PR already exists"
            echo "::set-output name=skip::true"
          else
            echo "::set-output name=skip::false"
          fi
  create_release_pr:
    name: Create release PR
    runs-on: ubuntu-20.04
    needs:
      - gather_facts
    if: ${{ needs.gather_facts.outputs.skip != 'true' }}
    env:
      architect_flags: "--organisation ${{ github.repository_owner }} --project ${{ github.event.repository.name }}"
    steps:
      - name: Install architect
        uses: giantswarm/install-binary-action@v1.0.0
        with:
          binary: "architect"
          version: "3.0.5"
      - name: Checkout code
        uses: actions/checkout@v2
      - name: Prepare release changes
        run: |
          architect prepare-release ${{ env.architect_flags }} --version "${{ needs.gather_facts.outputs.version }}"
      - name: Create release commit
        env:
          version: "${{ needs.gather_facts.outputs.version }}"
        run: |
          git config --local user.email "action@github.com"
          git config --local user.name "github-actions"
          git add -A
          git commit -m "Release v${{ env.version }}"
      - name: Push changes
        env:
          remote_repo: "https://${{ github.actor }}:${{ secrets.GITHUB_TOKEN }}@github.com/${{ github.repository }}.git"
        run: |
          git push "${remote_repo}" HEAD:${{ github.ref }}
      - name: Create PR
        env:
          GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
          base: "${{ needs.gather_facts.outputs.base }}"
          version: "${{ needs.gather_facts.outputs.version }}"
        run: |
          hub pull-request -f  -m "Release v${{ env.version }}" -a ${{ github.actor }} -b ${{ env.base }} -h ${{ github.event.ref }}
`
