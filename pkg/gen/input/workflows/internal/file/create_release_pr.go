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
    runs-on: ubuntu-18.04
    steps:
      - name: Print github context JSON
        run: |
          cat <<EOF
          ${{ toJson(github) }}
          EOF
  gather_facts:
    name: Gather facts
    runs-on: ubuntu-18.04
    outputs:
      base: ${{ steps.gather_facts.outputs.base }}
      version: ${{ steps.gather_facts.outputs.version }}
    steps:
      - name: Gather facts
        id: gather_facts
        run: |
          echo "::group::Get facts"
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
          echo "::endgroup::"
          echo "::group::Validate"
          git init -q
          git remote add origin ${{ github.event.repository.clone_url }}
          git fetch -q --depth=1 origin $base
          git fetch -q --depth=1 origin $head
          out=$(git rev-list --left-right --count origin/$base...origin/$head)
          ahead=$(echo $out | awk '{print $2}')
          behind=$(echo $out | awk '{print $1}')
          echo "ahead=\"$ahead\" behind=\"$behind\""
          if [[ $ahead -ne 0 ]] || [[ $behind -ne 0 ]] ; then
            echo "::error:: Branch $head is $ahead commits ahead and $behind commits behind $base branch. Please recreate the $head branch."
            exit 1
          fi
          echo "::endgroup::"
  create_release_pr:
    name: Create release PR
    runs-on: ubuntu-18.04
    needs:
      - gather_facts
    env:
      architect_flags: "--organisation ${{ github.repository_owner }} --project ${{ github.event.repository.name }}"
    steps:
      - name: Install tools
        uses: giantswarm/install-tools-action@v0.1.0
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
          git commit -m "release v${{ env.version }}"
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
          hub pull-request -f  -m "release v${{ env.version }}" -a ${{ github.actor }} -b ${{ env.base }} -h ${{ github.event.ref }}
`
