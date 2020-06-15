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
			Left:  "##-ignore-left-##",
			Right: "##-ignore-right-##",
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
  create:
    branches:
      - 'master#release#v*.*.*'
      - 'legacy#release#v*.*.*'
      - 'release-v*.x.x#release#v*.*.*'
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
  install_architect:
    name: Install architect
    runs-on: ubuntu-18.04
    env:
      BINARY: "architect"
      DIR: "/opt/cache"
      IMAGE: "quay.io/giantswarm/architect:1.2.0"
      IMAGE_PATH: "/usr/bin/architect"
    steps:
      - name: Cache
        id: cache
        uses: actions/cache@v1
        with:
          key: "install-${{ env.BINARY }}-${{ env.URL }}"
          path: "${{ env.DIR }}"
      - name: Download
        if: ${{ steps.cache.outputs.cache-hit != 'true' }}
        run: |
          mkdir -p ${{ env.DIR }}
          docker container create --name tmp ${{ env.IMAGE }}
          docker cp tmp:${{ env.IMAGE_PATH }} ${{ env.DIR }}/${{ env.BINARY }}
          docker container rm tmp
      - name: Smoke test
        run: |
          ${{ env.DIR }}/${{ env.BINARY }} version
      - name: Upload artifact
        uses: actions/upload-artifact@v1
        with:
          name: "${{ env.BINARY }}"
          path: "${{ env.DIR }}/${{ env.BINARY }}"
  install_hub:
    name: Install hub
    runs-on: ubuntu-18.04
    env:
      BINARY: "hub"
      DIR: "/opt/cache"
      URL: "https://github.com/github/hub/releases/download/v2.14.2/hub-linux-amd64-2.14.2.tgz"
    steps:
      - name: Cache
        id: cache
        uses: actions/cache@v1
        with:
          key: "install-${{ env.BINARY }}-${{ env.URL }}"
          path: "${{ env.DIR }}"
      - name: Download
        if: ${{ steps.cache.outputs.cache-hit != 'true' }}
        run: |
          mkdir ${{ env.DIR }}
          curl -fsSLo - ${{ env.URL }} | tar xvz --strip-components=1 --wildcards '*/bin/${{ env.BINARY }}'
          mv bin/${{ env.BINARY }} ${{ env.DIR }}
          chmod +x ${{ env.DIR }}/${{ env.BINARY }}
      - name: Smoke test
        run: |
          ${{ env.DIR }}/${{ env.BINARY }} version
      - name: Upload artifact
        uses: actions/upload-artifact@v1
        with:
          name: "${{ env.BINARY }}"
          path: "${{ env.DIR }}/${{ env.BINARY }}"
  create_release_pr:
    name: Create release PR
    runs-on: ubuntu-18.04
    needs:
      - install_architect
      - install_hub
    env:
      architect_flags: "--organisation ${{ github.repository_owner }} --project ${{ github.event.repository.name }}"
    steps:
      - name: Gather facts
        id: gather_facts
        run: |
          base="$(echo ${{ github.event.ref }} | cut -d '#' -f 1)"
          version="$(echo ${{ github.event.ref }} | cut -d '#' -f 3)"
          version="${version#v}" # Strip "v" prefix.
          echo "::set-output name=base::${base}"
          echo "::set-output name=version::${version}"
      - name: Download architect artifact to /opt/bin
        uses: actions/download-artifact@v2
        with:
          name: architect
          path: /opt/bin
      - name: Download hub artifact to /opt/bin
        uses: actions/download-artifact@v2
        with:
          name: hub
          path: /opt/bin
      - name: Prepare /opt/bin
        run: |
          chmod +x /opt/bin/*
          echo "::add-path::/opt/bin"
      - name: Print architect version
        run: |
          architect version ${{ env.architect_flags }}
      - name: Checkout code
        uses: actions/checkout@v2
      - name: Prepare release changes
        run: |
          architect prepare-release ${{ env.architect_flags }} --version "${{ steps.gather_facts.outputs.version }}"
      - name: Create release commit
        env:
          version: "${{ steps.gather_facts.outputs.version }}"
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
          base: "${{ steps.gather_facts.outputs.base }}"
          version: "${{ steps.gather_facts.outputs.version }}"
        run: |
          hub pull-request -f  -m "release v${{ env.version }}" -a ${{ github.actor }} -b ${{ env.base }} -h ${{ github.event.ref }}
`
