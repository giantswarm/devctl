package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

func NewCreateReleaseBranchInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create_release_branch.yaml"),
		TemplateBody: createReleaseBranchTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
	}

	return i
}

var createReleaseBranchTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen workflows
#

# Creates a branch for the 'previous' minor version when a 'new' minor version is tagged

name: create-minor-version-branch

on:
  push:
    tags: ['v*.*.*']

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

  create-release-branch:
    runs-on: ubuntu-18.04

    steps:
    - uses: actions/checkout@v2
      name: Check out the repository
      with:
        fetch-depth: 0  # Clone the whole history, not just the most recent commit.

    - name: Fetch all tags and branches
      run: "git fetch --all"

    - name: Set up python dependencies
      run: "pip3 install gitpython==3.1.3 semver==2.10.2"

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
        current_tag = os.environ.get("GITHUB_REF").strip('"')

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
`
