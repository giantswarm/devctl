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
			"CurrentFlavour":  p.CurrentFlavour,
			"FlavourApp":      p.FlavourApp,
			"FlavourCLI":      p.FlavourCLI,
			"FlavourLibrary":  p.FlavourLibrary,
			"FlavourOperator": p.FlavourOperator,
		},
	}

	return i
}

var createReleaseTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen workflows
#
name: Create Release
on:
  push:
    branches:
      - 'legacy'
      - 'master'
      - 'release-v*.*.x'
      # "!" negates previous positive patterns so it has to be at the end.
      - '!release-v*.x.x'
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
      project_go_path: ${{ steps.get_project_go_path.outputs.path }}
      version: ${{ steps.get_version.outputs.version }}
    steps:
      - name: Get version
        id: get_version
        run: |
          title="$(echo "${{ github.event.head_commit.message }}" | head -n 1 -)"
          # Matches strings like:
          #
          #   - "release v1.2.3"
          #   - "release v1.2.3-r4"
          #   - "release v1.2.3 (#56)"
          #   - "release v1.2.3-r4 (#56)"
          #
          # And outputs version part (1.2.3).
          if echo $title | grep -qE '^release v[0-9]+\.[0-9]+\.[0-9]+([.-][^ .-][^ ]*)?( \(#[0-9]+\))?$' ; then
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
  install_semver:
    name: Install semver
    runs-on: ubuntu-18.04
    env:
      BINARY: "semver"
      URL: "https://raw.githubusercontent.com/fsaintjacques/semver-tool/3.0.0/src/semver"
    steps:
      - name: Key
        id: key
        run: |
          cache_dir="/opt/cache"
          cache_key="install-${BINARY}-${URL}"
          echo "::set-output name=binary::${BINARY}"
          echo "::set-output name=cache_dir::${cache_dir}"
          echo "::set-output name=cache_key::${cache_key}"
          echo "::set-output name=url::${URL}"
      - name: Cache
        id: cache
        uses: actions/cache@v1
        with:
          key: "${{ steps.key.outputs.cache_key }}"
          path: "${{ steps.key.outputs.cache_dir }}"
      - name: Download
        if: ${{ steps.cache.outputs.cache-hit != 'true' }}
        run: |
          # TODO check hash
          binary="${{ steps.key.outputs.binary }}"
          cache_dir="${{ steps.key.outputs.cache_dir }}"
          url="${{ steps.key.outputs.url }}"
          mkdir $cache_dir
          curl -fsSLo $cache_dir/$binary $url
          chmod +x $cache_dir/$binary
      - name: Smoke test
        run: |
          ${{ steps.key.outputs.cache_dir }}/${{ steps.key.outputs.binary }} --version
      - name: Upload artifact
        uses: actions/upload-artifact@v1
        with:
          name: "${{ steps.key.outputs.binary }}"
          path: "${{ steps.key.outputs.cache_dir }}/${{ steps.key.outputs.binary }}"
  update_project_go:
    name: Update project.go
    runs-on: ubuntu-18.04
    if: ${{ needs.gather_facts.outputs.version != '' && needs.gather_facts.outputs.project_go_path != ''}}
    needs:
      - gather_facts
      - install_semver
    steps:
      - name: Download semver artifact to /opt/bin
        uses: actions/download-artifact@v2
        with:
          name: semver
          path: /opt/bin
      - name: Prepare /opt/bin
        run: |
          chmod +x /opt/bin/*
          echo "::add-path::/opt/bin"
      - name: Checkout code
        uses: actions/checkout@v2
      - name: Update project.go
        id: update_project_go
        run: |
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
          git commit -m "pkg/project: bump version to ${{ steps.update_project_go.outputs.new_version }}"
      - name: Push changes
        env:
          REMOTE_REPO: "https://${{ github.actor }}:${{ secrets.GITHUB_TOKEN }}@github.com/${{ github.repository }}.git"
        run: |
          git push "${REMOTE_REPO}" HEAD:${{ github.ref }}
  create_release:
    name: Create release
    runs-on: ubuntu-18.04
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
        if: ${{ needs.gather_facts.outputs.project_go_path != ''}}
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
    runs-on: ubuntu-18.04
    needs:
      - gather_facts
    if: ${{ needs.gather_facts.outputs.version }}
    steps:
      - uses: actions/checkout@v2
        name: Check out the repository
        with:
          fetch-depth: 0  # Clone the whole history, not just the most recent commit.

      - name: Fetch all tags and branches
        run: "git fetch --all"

      - name: Set up python dependencies
        run: "pip3 install gitpython==3.1.3 semver==2.10.2"

      - name: Set release version
        run: |
          export GITHUB_TAG_NAME=${{ needs.gather_facts.outputs.version }}
          echo "::set-env name=GITHUB_TAG_NAME::$GITHUB_TAG_NAME"

      - name: Create the script containing the branch logic
        run: |
          cat > ./create-branch.py <<-EOF
          # This script creates a branch for the previous minor version based on the current commit's parent
          # if the current tag introduces a new minor version and the release branch doesn't already exist.

          import os
          import semver
          from git import Repo

          p = "refs/tags/"  # GitHub tags start with this prefix

          # Takes a string and returns the string without the given prefix, if it is present
          def removeprefix(string: str, prefix: str) -> str:
              if string.startswith(prefix):
                  return string[len(prefix):]
              return string

          # Takes a string and returns the semver VersionInfo for that string, even if it includes a leading 'v'
          def version(string: str) -> semver.VersionInfo:
              string = removeprefix(string, 'v')
              return semver.VersionInfo.parse(string)

          repo = Repo(os.getcwd())  # Reference the repo from our current directory

          # Get the current tag from env and strip quotation marks
          current_tag = os.environ.get("GITHUB_TAG_NAME").strip('"')

          # Remove GitHub ref path
          current_tag = removeprefix(current_tag, p)
          print("Current tag is " + current_tag)

          # Get the tag of the "first" parent of the current commit including suffix, just for human reference
          parent_commit = repo.commit().parents[0]
          parent_tag = repo.git.describe('--tags', "{}".format(parent_commit))
          print("Parent commit tag was {}".format(parent_tag))

          # Get the closest tag to the parent commit (the tag version without the suffix)
          parent_tag = repo.git.describe('--tags', '--abbrev=0', "{}".format(parent_commit))

          # Get the semver for the parent tag
          parent_version = version(parent_tag)
          print("Parent base version was {}".format(parent_tag))

          # Get the semver for the current tag
          current_version = version(current_tag)

          # Check if the current tag version introduces a new major or minor version
          new_version = False
          if current_version.major > parent_version.major:
            print("Current tag is a new major version")
            new_version = True
          elif current_version.major == parent_version.major and current_version.minor > parent_version.minor:
            print("Current tag is a new minor version")
            new_version = True

          # Abort if not a new major or minor
          if not new_version:
            print("Current tag is not a new major or minor version.")
            print("Nothing to do here.")
            exit(0)

          # Format the expected name of the release branch for the previous minor version
          previous_branch_name = "release-v{}.{}.x".format(parent_version.major, parent_version.minor)
          print("Release branch for previous minor would be {}".format(previous_branch_name))

          # Check if the release branch already exists
          for b in repo.branches:
            if b.name == previous_branch_name:
              print("Release branch {} already exists. Nothing to do here.".format(previous_branch_name))
              exit(0)

          print("Release branch does not exist")

          # Create the branch
          print("Creating release branch {}".format(previous_branch_name))

          # Check out the parent commit to branch from
          origin = repo.remote()
          repo.git.checkout(parent_commit, force=True)  # Force parent checkout

          # Create a local branch from the parent commit
          release_branch = repo.create_head(previous_branch_name)

          # Push the local branch to remote
          # Unfortunately, no API way to do this - use the git client
          repo.git.push('--set-upstream', origin, previous_branch_name)
          EOF

      - name: Check and create release branch
        run: "python3 ./create-branch.py"

{{{{- if eq .CurrentFlavour .FlavourCLI }}}}

  install_architect:
    name: Install architect
    runs-on: ubuntu-18.04
    env:
      BINARY: "architect"
      DIR: "/opt/cache"
      IMAGE: "quay.io/giantswarm/architect"
      IMAGE_PATH: "/usr/bin/architect"
      VERSION: "1.2.0"
    outputs:
      cache_key: "${{ steps.get_cache_key.outputs.cache_key }}"
    steps:
      - name: Get cache key
        id: get_cache_key
        run: |
          cache_key="install-${{ env.BINARY }}-${{ env.VERSION }}"
          echo "::set-output name=cache_key::${cache_key}"
      - name: Cache
        id: cache
        uses: actions/cache@v1
        with:
          key: "${{ steps.get_cache_key.outputs.cache_key }}"
          path: "${{ env.DIR }}"
      - name: Download
        if: ${{ steps.cache.outputs.cache-hit != 'true' }}
        run: |
          mkdir -p ${{ env.DIR }}
          docker container create --name tmp ${{ env.IMAGE }}:${{ env.VERSION }}
          docker cp tmp:${{ env.IMAGE_PATH }} ${{ env.DIR }}/${{ env.BINARY }}
          docker container rm tmp
      - name: Smoke test
        run: |
          ${{ env.DIR }}/${{ env.BINARY }} version
      - name: Upload artifact
        uses: actions/upload-artifact@v2
        with:
          name: "${{ env.BINARY }}"
          path: "${{ env.DIR }}/${{ env.BINARY }}"
  create_and_upload_build_artifacts:
    name: Create and upload build artifacts
    runs-on: ubuntu-18.04
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
      - install_architect
    steps:
      - name: Cache
        id: cache
        uses: actions/cache@v1
        with:
          key: "${{ needs.install_architect.outputs.cache_key }}"
          path: /opt/bin
      - name: Download architect artifact to /opt/bin
        if: ${{ steps.cache.outputs.cache-hit != 'true' }}
        uses: actions/download-artifact@v2
        with:
          name: architect
          path: /opt/bin
      - name: Prepare /opt/bin
        run: |
          chmod +x /opt/bin/*
          echo "::add-path::/opt/bin"
      - name: Print architect version
        run: |
          architect version
      - name: Set up Go ${{ env.GO_VERSION }}
        uses: actions/setup-go@v1
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
          path: ${{ env.ARTIFACT_DIR }}
          upload_url: ${{ needs.create_release.outputs.upload_url }}
          asset_path: ${{ env.ARTIFACT_DIR }}/${{ env.FILE_NAME }}
          asset_name: ${{ env.FILE_NAME }}
          asset_content_type: application/octet-stream

{{{{- end }}}}
`
