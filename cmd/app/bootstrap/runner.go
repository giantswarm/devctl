package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/briandowns/spinner"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/google/go-github/v82/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
)

const (
	logLevelDebug = "debug"
	fileMode0600  = 0600
	fileMode0755  = 0755
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Writer = r.stdout

	// Create repository from app-template
	s.Suffix = " Creating repository from template..."
	s.Start()
	err := r.createRepository(ctx, r.flag.Name, "giantswarm", "template-app")
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Repository created from template")

	// Wait for repository to be fully created and initialized
	s.Suffix = " Waiting for repository to be initialized..."
	s.Start()
	time.Sleep(10 * time.Second)
	s.Stop()

	// Clone repository locally
	s.Suffix = " Cloning repository..."
	s.Start()
	repoPath, err := r.cloneRepository(ctx)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Repository cloned locally")

	// Replace placeholders
	s.Suffix = " Replacing placeholders..."
	s.Start()
	err = r.replacePlaceholders(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Placeholders replaced")

	// Configure sync method (vendir/kustomize)
	s.Suffix = fmt.Sprintf(" Configuring sync method (%s)...", r.flag.SyncMethod)
	s.Start()
	err = r.configureSyncMethod(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Sync method configured")

	// Configure patch method (script/kustomize)
	s.Suffix = fmt.Sprintf(" Configuring patch method (%s)...", r.flag.PatchMethod)
	s.Start()
	err = r.configurePatchMethod(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Patch method configured")

	// Setup CI/CD without branch protection
	s.Suffix = " Setting up CI/CD..."
	s.Start()
	err = r.setupCICD(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ CI/CD setup complete")

	// Generate workflows and Makefile
	s.Suffix = " Generating workflows and Makefile..."
	s.Start()
	err = r.generateWorkflowsAndMakefile(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Workflows and Makefile generated")

	// Create PR for giantswarm/github repository
	s.Suffix = " Creating PR for giantswarm/github..."
	s.Start()
	var prURL string
	err, prURL = r.createGithubRepoPR(ctx)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ PR created in giantswarm/github")

	// Initial commit and push
	s.Suffix = " Pushing changes to main branch..."
	s.Start()
	err = r.commitAndPush(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Changes pushed to main branch")

	// Enable branch protection
	s.Suffix = " Enabling branch protection..."
	s.Start()
	err = r.enableBranchProtection(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Branch protection enabled")

	fmt.Fprintf(r.stdout, "\n✨ Successfully bootstrapped app repository %s\n\n", r.flag.Name)
	fmt.Fprintf(r.stdout, "Next steps:\n")
	fmt.Fprintf(r.stdout, "1. Visit your new repository: https://github.com/giantswarm/%s-app\n", r.flag.Name)
	if prURL != "" {
		fmt.Fprintf(r.stdout, "2. Review and merge the PR: %s\n", prURL)
	} else {
		fmt.Fprintf(r.stdout, "2. Review and merge the PR: https://github.com/giantswarm/github/pulls\n")
	}
	fmt.Fprintf(r.stdout, "3. Update the Chart.yaml with appropriate metadata and version\n")
	fmt.Fprintf(r.stdout, "4. Configure your image registry in values.yaml\n")
	fmt.Fprintf(r.stdout, "5. Create a release by pushing a tag (e.g., v0.1.0)\n")
	fmt.Fprintf(r.stdout, "\nFor more information, visit: https://intranet.giantswarm.io/docs/dev-and-releng/app-developer-guide/\n")

	return nil
}

func (r *runner) createRepository(ctx context.Context, name string, owner string, templateRepo string) error {
	token := os.Getenv(r.flag.GithubToken)
	if token == "" {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q not found", r.flag.GithubToken)
	}

	// Create a logger that only outputs in debug mode
	logger := logrus.New()
	if os.Getenv("LOG_LEVEL") == logLevelDebug {
		logger.SetOutput(r.stdout)
	} else {
		logger.SetOutput(io.Discard)
	}

	config := githubclient.Config{
		Logger:      logger,
		AccessToken: token,
		DryRun:      r.flag.DryRun,
	}

	client, err := githubclient.New(config)
	if err != nil {
		return microerror.Mask(err)
	}

	repoName := fmt.Sprintf("%s-app", name)
	repo := &github.Repository{
		Name:        github.Ptr(repoName),
		Private:     github.Ptr(false),
		Description: github.Ptr(fmt.Sprintf("Helm chart for %s", name)),
	}

	_, err = client.CreateFromTemplate(ctx, owner, templateRepo, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) cloneRepository(ctx context.Context) (string, error) {
	// Clone repository locally
	repoPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-app", r.flag.Name))
	repoURL := fmt.Sprintf("git@github.com:giantswarm/%s-app.git", r.flag.Name)

	// Remove existing directory if it exists
	_ = os.RemoveAll(repoPath)

	// Wait for repository to be populated with template content
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		err := r.execCommand(ctx, "", "git", "clone", repoURL, repoPath)
		if err != nil {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Failed to clone repository (attempt %d/%d): %v", i+1, maxRetries, err))

			// Check if the directory exists and remove it before retrying
			_ = os.RemoveAll(repoPath)

			// Wait before retrying
			if i < maxRetries-1 {
				time.Sleep(5 * time.Second)
				continue
			}
			return "", microerror.Mask(err)
		}

		// Check if the repository has content
		if _, err := os.Stat(filepath.Join(repoPath, "helm")); err == nil {
			break
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("Repository not yet populated with template content (attempt %d/%d)", i+1, maxRetries))

		// Remove the empty repository and retry
		_ = os.RemoveAll(repoPath)

		if i < maxRetries-1 {
			time.Sleep(5 * time.Second)
			continue
		}
		return "", microerror.Maskf(executionFailedError, "repository not populated with template content after %d attempts", maxRetries)
	}

	return repoPath, nil
}

func (r *runner) configureSyncMethod(ctx context.Context, repoPath string) error {
	switch r.flag.SyncMethod {
	case "vendir":
		return r.configureVendir(ctx, repoPath)
	case "kustomize":
		return r.configureKustomize(ctx, repoPath)
	default:
		return microerror.Maskf(invalidFlagError, "unsupported sync method: %s", r.flag.SyncMethod)
	}
}

func (r *runner) configureVendir(ctx context.Context, repoPath string) error {
	// Create vendir.yml - main needs to be replaced with the latest upstream release (eg &version "v1.17.2")
	vendirConfig := fmt.Sprintf(`apiVersion: vendir.k14s.io/v1alpha1
kind: Config
minimumRequiredVersion: 0.12.0
directories:
- path: vendor
  contents:
  - path: .
    git:
      url: %s
      ref: main
    includePaths:
    - %s/**/*
- path: helm/%s/templates
  contents:
  - path: .
    directory:
      path: vendor/%s/templates
`,
		r.flag.UpstreamRepo,
		r.flag.UpstreamChart,
		r.flag.Name,
		r.flag.UpstreamChart)

	err := os.WriteFile(filepath.Join(repoPath, "vendir.yml"), []byte(vendirConfig), fileMode0600)
	if err != nil {
		return microerror.Mask(err)
	}

	fmt.Printf("Repo path %s \n vendir.yml file created \n", repoPath)

	// Run initial sync
	err = r.execCommand(ctx, repoPath, "vendir", "sync")
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) configureKustomize(ctx context.Context, repoPath string) error {
	// TODO: Implement kustomize configuration
	return nil
}

func (r *runner) configurePatchMethod(ctx context.Context, repoPath string) error {
	switch r.flag.PatchMethod {
	case "script":
		return r.configurePatchScript(ctx, repoPath)
	case "kustomize":
		return r.configurePatchKustomize(ctx, repoPath)
	default:
		return microerror.Maskf(invalidFlagError, "unsupported patch method: %s", r.flag.PatchMethod)
	}
}

func (r *runner) configurePatchScript(ctx context.Context, repoPath string) error {
	// Create sync directory and patches subdirectory
	syncDir := filepath.Join(repoPath, "sync")
	patchesDir := filepath.Join(syncDir, "patches")
	err := os.MkdirAll(patchesDir, fileMode0755)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create sync.sh script
	syncScript := `#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
cd "${dir}/.."

# Stage 1 sync - intermediate to the ./vendir folder
set -x
vendir sync
helm dependency update helm/%s/
{ set +x; } 2>/dev/null

# Apply patches
for patch in sync/patches/*; do
    if [ -f "$patch" ]; then
        ./sync/patches/$(basename "$patch")/patch.sh
    fi
done

# Store diffs
rm -f ./diffs/*
for f in $(git --no-pager diff --no-exit-code --no-color --no-index vendor/%s helm --name-only) ; do
        [[ "$f" == "helm/%s/Chart.yaml" ]] && continue
        [[ "$f" == "helm/%s/Chart.lock" ]] && continue
        [[ "$f" == "helm/%s/README.md" ]] && continue
        [[ "$f" == "helm/%s/values.schema.json" ]] && continue
        [[ "$f" == "helm/%s/values.yaml" ]] && continue
        [[ "$f" =~ ^helm/%s/charts/.* ]] && continue

        base_file="vendor/%s/${f#"helm/"}"
        [[ ! -e $base_file ]] && base_file="/dev/null"

        set +e
        set -x
        git --no-pager diff --no-exit-code --no-color --no-index "$base_file" "${f}" \
                > "./diffs/${f//\//__}.patch"
        { set +x; } 2>/dev/null
        set -e
        ret=$?
        if [ $ret -ne 0 ] && [ $ret -ne 1 ] ; then
                exit $ret
        fi
done`

	syncScript = fmt.Sprintf(syncScript,
		r.flag.Name, r.flag.Name, r.flag.Name, r.flag.Name,
		r.flag.Name, r.flag.Name, r.flag.Name, r.flag.Name, r.flag.Name)

	err = os.WriteFile(filepath.Join(syncDir, "sync.sh"), []byte(syncScript), fileMode0755)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) configurePatchKustomize(ctx context.Context, repoPath string) error {
	// TODO: Implement kustomize patch configuration
	return nil
}

func (r *runner) replacePlaceholders(ctx context.Context, repoPath string) error {
	// Rename helm chart directory
	oldPath := filepath.Join(repoPath, "helm", "{APP-NAME}")
	newPath := filepath.Join(repoPath, "helm", r.flag.Name)
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return microerror.Mask(err)
	}

	// Determine the correct sed -i flag based on the OS
	var sedInPlaceFlag string
	if runtime.GOOS == "darwin" {
		sedInPlaceFlag = "sed -i ''" // macOS requires an empty string after -i
	} else {
		sedInPlaceFlag = "sed -i" // Linux does not require an empty string
	}

	replaceString := fmt.Sprintf("\"s|github.com/giantswarm/{APP-NAME}|github.com/giantswarm/%s-app|g\"", r.flag.Name)
	// First replace GitHub URLs that need the -app suffix
	err = r.execCommand(ctx, repoPath,
		"find", ".", "-path", "\"./.git\"", "-prune",
		"-o", "-type", "f", "-exec", sedInPlaceFlag,
		replaceString, "{}", "+")
	if err != nil {
		return microerror.Mask(err)
	}

	// Then replace CircleCI URLs that need the -app suffix
	err = r.execCommand(ctx, repoPath,
		"find", ".", "-path", "'./.git'", "-prune",
		"-o", "-type", "f", "-exec", sedInPlaceFlag,
		fmt.Sprintf("\"s|gh/giantswarm/{APP-NAME}/|gh/giantswarm/%s-app/|g\"", r.flag.Name),
		"{}", "+")
	if err != nil {
		return microerror.Mask(err)
	}

	// Then do the general replacement for all other cases
	err = r.execCommand(ctx, repoPath,
		"find", ".", "-path", "'./.git'", "-prune",
		"-o", "-type", "f", "-exec", sedInPlaceFlag,
		fmt.Sprintf("\"s/{APP-NAME}/%s/g\"", r.flag.Name), "{}", "+")
	if err != nil {
		return microerror.Mask(err)
	}

	// Replace team in CODEOWNERS
	err = r.execCommand(ctx, repoPath,
		sedInPlaceFlag,
		fmt.Sprintf("s/@giantswarm\\/team-honeybadger/@giantswarm\\/team-%s/g", r.flag.Team),
		"CODEOWNERS")
	if err != nil {
		return microerror.Mask(err)
	}

	// Add team label
	err = r.execCommand(ctx, repoPath,
		sedInPlaceFlag,
		fmt.Sprintf(`s/app.kubernetes.io\/name: %s/app.kubernetes.io\/name: %s\n    application.giantswarm.io\/team: %s/`,
			r.flag.Name, r.flag.Name, r.flag.Team),
		fmt.Sprintf("helm/%s/templates/_helpers.tpl", r.flag.Name))
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) setupCICD(ctx context.Context, repoPath string) error {
	// Get token from custom environment variable
	token := os.Getenv(r.flag.GithubToken)
	if token == "" {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q not found", r.flag.GithubToken)
	}

	// Run devctl repo setup with GITHUB_TOKEN set
	repoFullName := fmt.Sprintf("giantswarm/%s-app", r.flag.Name)
	cmd := exec.CommandContext(ctx, "devctl", "repo", "setup", repoFullName, "--disable-branch-protection")
	cmd.Dir = repoPath

	// Only show output in debug mode
	if os.Getenv("LOG_LEVEL") == logLevelDebug {
		cmd.Stdout = r.stdout
		cmd.Stderr = r.stderr
	}

	cmd.Env = append(os.Environ(), fmt.Sprintf("GITHUB_TOKEN=%s", token))

	err := cmd.Run()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) commitAndPush(ctx context.Context, repoPath string) error {
	err := r.execCommand(ctx, repoPath, "git", "add", "-A")
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.execCommand(ctx, repoPath, "git", "commit", "-m", "Bootstrap app repository")
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.execCommand(ctx, repoPath, "git", "push", "origin", "main")
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) enableBranchProtection(ctx context.Context, repoPath string) error {
	// Get token from custom environment variable
	token := os.Getenv(r.flag.GithubToken)
	if token == "" {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q not found", r.flag.GithubToken)
	}

	// Run devctl repo setup with GITHUB_TOKEN set (without --disable-branch-protection)
	repoFullName := fmt.Sprintf("giantswarm/%s-app", r.flag.Name)
	cmd := exec.CommandContext(ctx, "devctl", "repo", "setup", repoFullName)
	cmd.Dir = repoPath

	// Only show output in debug mode
	if os.Getenv("LOG_LEVEL") == logLevelDebug {
		cmd.Stdout = r.stdout
		cmd.Stderr = r.stderr
	}

	cmd.Env = append(os.Environ(), fmt.Sprintf("GITHUB_TOKEN=%s", token))

	err := cmd.Run()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) execCommand(ctx context.Context, dir string, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Only show command output in debug mode
	if os.Getenv("LOG_LEVEL") == logLevelDebug {
		cmd.Stdout = r.stdout
		cmd.Stderr = r.stderr
	}

	return cmd.Run()
}

func (r *runner) generateWorkflowsAndMakefile(ctx context.Context, repoPath string) error {
	// Generate workflows
	err := r.execCommand(ctx, repoPath, "devctl", "gen", "workflows",
		"--flavour", "app",
		"--install-update-chart")
	if err != nil {
		return microerror.Mask(err)
	}

	// Generate Makefile
	err = r.execCommand(ctx, repoPath, "devctl", "gen", "makefile",
		"--flavour", "app",
		"--language", "generic")
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) createGithubRepoPR(ctx context.Context) (error, string) {
	token := os.Getenv(r.flag.GithubToken)
	if token == "" {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q not found", r.flag.GithubToken), ""
	}

	// Create a logger that only outputs in debug mode
	logger := logrus.New()
	if os.Getenv("LOG_LEVEL") == logLevelDebug {
		logger.SetOutput(r.stdout)
	} else {
		logger.SetOutput(io.Discard)
	}

	// Setup GitHub client
	config := githubclient.Config{
		Logger:      logger,
		AccessToken: token,
		DryRun:      r.flag.DryRun,
	}

	client, err := githubclient.New(config)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Clone giantswarm/github repository
	repoPath := filepath.Join(os.TempDir(), "github")
	_ = os.RemoveAll(repoPath) // Clean up any existing directory

	err = r.execCommand(ctx, "", "git", "clone", "git@github.com:giantswarm/github.git", repoPath)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Create new branch
	branchName := fmt.Sprintf("add-%s-app", r.flag.Name)
	err = r.execCommand(ctx, repoPath, "git", "checkout", "-b", branchName)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Update team YAML file
	teamFile := filepath.Join(repoPath, "repositories", fmt.Sprintf("team-%s.yaml", r.flag.Team))
	newEntry := fmt.Sprintf(`- name: %s-app
  componentType: service
  gen:
    flavours:
      - app
    language: generic
    installUpdateChart: true
`, r.flag.Name)

	// Read existing file
	content, err := os.ReadFile(teamFile)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Parse YAML as a list, preserving comments
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	var repositories []map[string]interface{}
	err = decoder.Decode(&repositories)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Parse new entry
	var newEntries []map[string]interface{}
	err = yaml.Unmarshal([]byte(newEntry), &newEntries)
	if err != nil {
		return microerror.Mask(err), ""
	}
	newRepo := newEntries[0]

	// Insert entry in alphabetical order
	inserted := false
	for i, repo := range repositories {
		if repo["name"].(string) > fmt.Sprintf("%s-app", r.flag.Name) {
			repositories = append(repositories[:i], append([]map[string]interface{}{newRepo}, repositories[i:]...)...)
			inserted = true
			break
		}
	}
	if !inserted {
		repositories = append(repositories, newRepo)
	}

	// Add YAML header comment and marshal with proper indentation
	var buf bytes.Buffer
	buf.WriteString("# yaml-language-server: $schema=../.github/repositories.schema.json\n")
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	err = encoder.Encode(repositories)
	if err != nil {
		return microerror.Mask(err), ""
	}

	err = os.WriteFile(teamFile, buf.Bytes(), fileMode0600)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Commit changes
	err = r.execCommand(ctx, repoPath, "git", "add", teamFile)
	if err != nil {
		return microerror.Mask(err), ""
	}

	err = r.execCommand(ctx, repoPath, "git", "commit", "-m", fmt.Sprintf("Add %s-app to team-%s repositories", r.flag.Name, r.flag.Team))
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Push changes using token
	err = r.execCommand(ctx, repoPath, "git", "push", "origin", branchName)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Create pull request using githubclient
	prTitle := fmt.Sprintf("Add %s-app to team-%s repositories", r.flag.Name, r.flag.Team)

	createdPR, err := client.CreatePullRequest(ctx, "giantswarm", "github", branchName, prTitle)
	if err != nil {
		return microerror.Mask(err), ""
	}

	// Store PR URL for final message
	if createdPR.HTMLURL != nil {
		return nil, *createdPR.HTMLURL
	}
	return nil, ""
}
