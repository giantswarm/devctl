package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/briandowns/spinner"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/google/go-github/v70/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
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

	// 1. Create repository from app-template
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

	// 2. Clone repository locally
	s.Suffix = " Cloning repository..."
	s.Start()
	repoPath, err := r.cloneRepository(ctx)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Repository cloned locally")

	// 3. Configure sync method (vendir/kustomize)
	s.Suffix = fmt.Sprintf(" Configuring sync method (%s)...", r.flag.SyncMethod)
	s.Start()
	err = r.configureSyncMethod(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Sync method configured")

	// 4. Configure patch method (script/kustomize)
	s.Suffix = fmt.Sprintf(" Configuring patch method (%s)...", r.flag.PatchMethod)
	s.Start()
	err = r.configurePatchMethod(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Patch method configured")

	// 5. Replace placeholders
	s.Suffix = " Replacing placeholders..."
	s.Start()
	err = r.replacePlaceholders(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Placeholders replaced")

	// 6. Setup CI/CD
	s.Suffix = " Setting up CI/CD..."
	s.Start()
	err = r.setupCICD(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ CI/CD setup complete")

	// 7. Initial commit and push
	s.Suffix = " Creating pull request..."
	s.Start()
	err = r.commitAndPush(ctx, repoPath)
	if err != nil {
		s.Stop()
		return microerror.Mask(err)
	}
	s.Stop()
	fmt.Fprintln(r.stdout, "✓ Pull request created")

	fmt.Fprintf(r.stdout, "\n✨ Successfully bootstrapped app repository %s\n", r.flag.Name)

	return nil
}

func (r *runner) createRepository(ctx context.Context, name string, owner string, templateRepo string) error {
	token := os.Getenv(r.flag.GithubToken)
	if token == "" {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q not found", r.flag.GithubToken)
	}

	// Create a logger that only outputs in debug mode
	logger := logrus.New()
	if os.Getenv("LOG_LEVEL") == "debug" {
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
		Name:        github.String(repoName),
		Private:     github.Bool(false),
		Description: github.String(fmt.Sprintf("Helm chart for %s", name)),
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
	vendirConfig := fmt.Sprintf(`apiVersion: vendir.k14s.io/v1alpha1
kind: Config
minimumRequiredVersion: 0.12.0
directories:
- path: vendor
  contents:
  - path: %s
    git:
      url: %s
      ref: origin/main
    includePaths:
    - %s/**/*`, r.flag.Name, r.flag.UpstreamRepo, r.flag.UpstreamChart)

	err := os.WriteFile(filepath.Join(repoPath, "vendir.yml"), []byte(vendirConfig), 0644)
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
	err := os.MkdirAll(patchesDir, 0755)
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

	err = os.WriteFile(filepath.Join(syncDir, "sync.sh"), []byte(syncScript), 0755)
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

	// Replace {APP-NAME} with actual app name in all files, excluding .git directory
	err = r.execCommand(ctx, repoPath,
		"find", ".", "-type", "f",
		"-not", "-path", "./.git/*",
		"-exec", "sed", "-i", fmt.Sprintf("s/{APP-NAME}/%s/g", r.flag.Name), "{}", "+")
	if err != nil {
		return microerror.Mask(err)
	}

	// Add team label
	err = r.execCommand(ctx, repoPath,
		"sed", "-i",
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
	cmd := exec.CommandContext(ctx, "devctl", "repo", "setup", repoFullName)
	cmd.Dir = repoPath

	// Only show output in debug mode
	if os.Getenv("LOG_LEVEL") == "debug" {
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
	// Create and checkout feature branch
	branchName := "bootstrap-app"
	err := r.execCommand(ctx, repoPath, "git", "checkout", "-b", branchName)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.execCommand(ctx, repoPath, "git", "add", "-A")
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.execCommand(ctx, repoPath, "git", "commit", "-m", "Bootstrap app repository")
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.execCommand(ctx, repoPath, "git", "push", "origin", branchName)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create pull request
	token := os.Getenv(r.flag.GithubToken)
	if token == "" {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q not found", r.flag.GithubToken)
	}

	logger := logrus.New()
	logger.SetOutput(r.stdout)

	config := githubclient.Config{
		Logger:      logger,
		AccessToken: token,
		DryRun:      r.flag.DryRun,
	}

	client, err := githubclient.New(config)
	if err != nil {
		return microerror.Mask(err)
	}

	repoName := fmt.Sprintf("%s-app", r.flag.Name)
	title := "Bootstrap app repository"
	body := "Initial bootstrap of the app repository"
	pr := &github.NewPullRequest{
		Title:               github.String(title),
		Head:                github.String(branchName),
		Base:                github.String("main"),
		Body:                github.String(body),
		MaintainerCanModify: github.Bool(true),
	}

	_, err = client.CreatePullRequest(ctx, "giantswarm", repoName, pr)
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
	if os.Getenv("LOG_LEVEL") == "debug" {
		cmd.Stdout = r.stdout
		cmd.Stderr = r.stderr
	}

	return cmd.Run()
}
