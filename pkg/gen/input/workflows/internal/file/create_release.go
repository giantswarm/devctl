package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

func NewCreateReleaseInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create_release.yaml"),
		TemplateBody: createReleaseTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":       params.Header("#"),
			"IsFlavourCLI": params.IsFlavourCLI(p),
		},
	}

	return i
}

var createReleaseTemplate = `{{{{ .Header }}}}
name: Create Release
on:
  push:
    branches:
      - 'legacy'
	  - 'main'
      - 'master'
      - 'release-v*.*.x'
      # "!" negates previous positive patterns so it has to be at the end.
      - '!release-v*.x.x'
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
      project_go_path: ${{ steps.get_project_go_path.outputs.path }}
      ref_version: ${{ steps.ref_version.outputs.refversion }}
      version: ${{ steps.get_version.outputs.version }}
    steps:
      - name: Get version
        id: get_version
        run: |
          title="$(echo "${{ github.event.head_commit.message }}" | head -n 1 -)"
          # Matches strings like:
          #
          #   - "Release v1.2.3"
          #   - "Release v1.2.3-r4"
          #   - "Release v1.2.3 (#56)"
          #   - "Release v1.2.3-r4 (#56)"
          #
          # And outputs version part (1.2.3).
          if echo $title | grep -iqE '^Release v[0-9]+\.[0-9]+\.[0-9]+([.-][^ .-][^ ]*)?( \(#[0-9]+\))?$' ; then
            version=$(echo $title | cut -d ' ' -f 2)
          fi
          version="${version#v}" # Strip "v" prefix.
          echo "version=\"$version\""
          echo "::set-output name=version::${version}"
      - name: Checkout code
        if: ${{ steps.get_version.outputs.version != '' }}
        uses: actions/checkout@v2
      - name: Get project.go path
        id: get_project_go_path
        if: ${{ steps.get_version.outputs.version != '' }}
        run: |
          path='./pkg/project/project.go'
          if [[ ! -f $path ]] ; then
            path=''
          fi
          echo "path=\"$path\""
          echo "::set-output name=path::${path}"
      - name: Check if reference version
        id: ref_version
        run: |
          title="$(echo "${{ github.event.head_commit.message }}" | head -n 1 -)"
          if echo $title | grep -qE '^release v[0-9]+\.[0-9]+\.[0-9]+([.-][^ .-][^ ]*)?( \(#[0-9]+\))?$' ; then
            version=$(echo $title | cut -d ' ' -f 2)
          fi
          version=$(echo $title | cut -d ' ' -f 2)
          version="${version#v}" # Strip "v" prefix.
          refversion=false
          if [[ "${version}" =~ ^[0-9]+.[0-9]+.[0-9]+-[0-9]+$ ]]; then
            refversion=true
          fi
          echo "refversion =\"$refversion\""
          echo "::set-output name=refversion::$refversion"
  update_project_go:
    name: Update project.go
    runs-on: ubuntu-20.04
    if: ${{ needs.gather_facts.outputs.version != '' && needs.gather_facts.outputs.project_go_path != '' && needs.gather_facts.outputs.ref_version != 'true' }}
    needs:
      - gather_facts
    steps:
      - name: Install architect
        uses: giantswarm/install-binary-action@v1.0.0
        with:
          binary: "architect"
          version: "3.0.5"
      - name: Install semver
        uses: giantswarm/install-binary-action@v1.0.0
        with:
          binary: "semver"
          version: "3.0.0"
          download_url: "https://github.com/fsaintjacques/${binary}-tool/archive/${version}.tar.gz"
          tarball_binary_path: "*/src/${binary}"
          smoke_test: "${binary} --version"
      - name: Checkout code
        uses: actions/checkout@v2
      - name: Update project.go
        id: update_project_go
        env:
          branch: "${{ github.ref }}-version-bump"
        run: |
          git checkout -b ${{ env.branch }}
          file="${{ needs.gather_facts.outputs.project_go_path }}"
          version="${{ needs.gather_facts.outputs.version }}"
          new_version="$(semver bump patch $version)-dev"
          echo "version=\"$version\" new_version=\"$new_version\""
          echo "::set-output name=new_version::${new_version}"
          sed -Ei "s/(version[[:space:]]*=[[:space:]]*)\"${version}\"/\1\"${new_version}\"/" $file
          if git diff --exit-code $file ; then
            echo "error: no changes in \"$file\"" >&2
            exit 1
          fi
      - name: Commit changes
        run: |
          file="${{ needs.gather_facts.outputs.project_go_path }}"
          git config --local user.email "action@github.com"
          git config --local user.name "GitHub Action"
          git add $file
          git commit -m "Bump version to ${{ steps.update_project_go.outputs.new_version }}"
      - name: Push changes
        env:
          REMOTE_REPO: "https://${{ github.actor }}:${{ secrets.GITHUB_TOKEN }}@github.com/${{ github.repository }}.git"
          branch: "${{ github.ref }}-version-bump"
        run: |
          git push "${REMOTE_REPO}" HEAD:${{ env.branch }}
      - name: Create PR
        env:
          GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}"
          base: "${{ github.ref }}"
          branch: "${{ github.ref }}-version-bump"
          version: "${{ needs.gather_facts.outputs.version }}"
          title: "Bump version to ${{ steps.update_project_go.outputs.new_version }}"
        run: |
          hub pull-request -f  -m "${{ env.title }}" -b ${{ env.base }} -h ${{ env.branch }} -r ${{ github.actor }}
  create_release:
    name: Create release
    runs-on: ubuntu-20.04
    needs:
      - gather_facts
    if: ${{ needs.gather_facts.outputs.version }}
    outputs:
      upload_url: ${{ steps.create_gh_release.outputs.upload_url }}
    steps:
      - name: Checkout code
        uses: actions/checkout@v2
        with:
          ref: ${{ github.sha }}
      - name: Ensure correct version in project.go
        if: ${{ needs.gather_facts.outputs.project_go_path != '' && needs.gather_facts.outputs.ref_version != 'true' }}
        run: |
          file="${{ needs.gather_facts.outputs.project_go_path }}"
          version="${{ needs.gather_facts.outputs.version }}"
          grep -qE "version[[:space:]]*=[[:space:]]*\"$version\"" $file
      - name: Create tag
        run: |
          version="${{ needs.gather_facts.outputs.version }}"
          git config --local user.name "github-actions"
          git tag "v$version" ${{ github.sha }}
      - name: Push tag
        env:
          REMOTE_REPO: "https://${{ github.actor }}:${{ secrets.GITHUB_TOKEN }}@github.com/${{ github.repository }}.git"
        run: |
          git push "${REMOTE_REPO}" --tags
      - name: Create release
        id: create_gh_release
        uses: actions/create-release@v1
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        with:
          tag_name: "v${{ needs.gather_facts.outputs.version }}"
          release_name: "v${{ needs.gather_facts.outputs.version }}"
  create-release-branch:
    name: Create release branch
    runs-on: ubuntu-20.04
    needs:
      - gather_facts
    if: ${{ needs.gather_facts.outputs.version }}
    steps:
      - name: Install semver
        uses: giantswarm/install-binary-action@v1.0.0
        with:
          binary: "semver"
          version: "3.0.0"
          download_url: "https://github.com/fsaintjacques/${binary}-tool/archive/${version}.tar.gz"
          tarball_binary_path: "*/src/${binary}"
          smoke_test: "${binary} --version"
      - name: Check out the repository
        uses: actions/checkout@v2
        with:
          fetch-depth: 0  # Clone the whole history, not just the most recent commit.
      - name: Fetch all tags and branches
        run: "git fetch --all"
      - name: Create long-lived release branch
        run: |
          current_version="${{ needs.gather_facts.outputs.version }}"
          parent_version="$(git describe --tags --abbrev=0 HEAD^ || true)"
          parent_version="${parent_version#v}" # Strip "v" prefix.

          if [[ -z "$parent_version" ]] ; then
            echo "Unable to find a parent tag version. No branch to create."
            exit 0
          fi

          echo "current_version=$current_version parent_version=$parent_version"

          current_major=$(semver get major $current_version)
          current_minor=$(semver get minor $current_version)
          parent_major=$(semver get major $parent_version)
          parent_minor=$(semver get minor $parent_version)
          echo "current_major=$current_major current_minor=$current_minor parent_major=$parent_major parent_minor=$parent_minor"

          if [[ $current_major -gt $parent_major ]] ; then
            echo "Current tag is a new major version"
          elif [[ $current_major -eq $parent_major ]] && [[ $current_minor -gt $parent_minor ]] ; then
            echo "Current tag is a new minor version"
          else
            echo "Current tag is not a new major or minor version. Nothing to do here."
            exit 0
          fi

          release_branch="release-v${parent_major}.${parent_minor}.x"
          echo "release_branch=$release_branch"

          if git rev-parse --verify $release_branch ; then
            echo "Release branch $release_branch already exists. Nothing to do here."
            exit 0
          fi

          git branch $release_branch HEAD^
          git push origin $release_branch

{{{{- if .IsFlavourCLI }}}}

  create_and_upload_build_artifacts:
    name: Create and upload build artifacts
    runs-on: ubuntu-20.04
    strategy:
      matrix:
        platform:
          - darwin
          - linux
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GO_VERSION: 1.14.2
      ARTIFACT_DIR: bin-dist
      TAG: v${{ needs.gather_facts.outputs.version }}
    needs:
      - create_release
      - gather_facts
    steps:
      - name: Install architect
        uses: giantswarm/install-binary-action@v1.0.0
        with:
          binary: "architect"
          version: "3.0.5"
      - name: Set up Go ${{ env.GO_VERSION }}
        uses: actions/setup-go@v2.1.3
        with:
          go-version: ${{ env.GO_VERSION }}
      - name: Checkout code
        uses: actions/checkout@v2
        with:
          ref: ${{ env.TAG }}
      - name: Create ${{ matrix.platform }} package
        run: make package-${{ matrix.platform }}
      - name: Add ${{ matrix.platform }} package to release
        uses: actions/upload-release-asset@v1
        env:
          FILE_NAME: ${{ github.event.repository.name }}-${{ env.TAG }}-${{ matrix.platform }}-amd64.tar.gz
        with:
          upload_url: ${{ needs.create_release.outputs.upload_url }}
          asset_path: ${{ env.ARTIFACT_DIR }}/${{ env.FILE_NAME }}
          asset_name: ${{ env.FILE_NAME }}
          asset_content_type: application/octet-stream

{{{{- end }}}}
`
