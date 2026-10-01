// Package engine is what the repo commands share to run the set-up steps of
// pkg/reposetup/reconcile as the person: the clients from the token
// environment variables, the repository slug, the log adapter and the
// result on stdout.
package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

const (
	// DefaultGitHubEnvVar names the environment variable the GitHub token is
	// read from unless a flag names another.
	DefaultGitHubEnvVar = "GITHUB_TOKEN"
	// DefaultCircleCIEnvVar names the environment variable the CircleCI token
	// is read from unless a flag names another.
	DefaultCircleCIEnvVar = "CIRCLECI_TOKEN"

	// OutputTable renders the result as a table, OutputJSON as the
	// structured value.
	OutputTable = "table"
	OutputJSON  = "json"
)

// Token returns the token in $tokenEnv; an unset variable is an error.
func Token(tokenEnv string) (string, error) {
	token, found := os.LookupEnv(tokenEnv)
	if !found {
		return "", microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", tokenEnv)
	}
	return token, nil
}

// GitHubClient returns the GitHub client for the token in $tokenEnv, sending
// through transport (nil: the default transport; a reconcile.Counter to
// count the requests).
func GitHubClient(logger *logrus.Logger, tokenEnv string, dryRun bool, transport http.RoundTripper) (*githubclient.Client, error) {
	token, err := Token(tokenEnv)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	client, err := githubclient.New(githubclient.Config{
		Logger:      logger,
		AccessToken: token,
		DryRun:      dryRun,
		Transport:   transport,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return client, nil
}

// CircleCIClient returns the CircleCI client for the token in $tokenEnv,
// sending through transport (nil: the default transport; a reconcile.Counter
// to count the requests), or nil when the variable is unset: the steps that
// need CircleCI are then skipped, which the log says.
func CircleCIClient(logger *logrus.Logger, tokenEnv string, transport http.RoundTripper) (*circleciclient.Client, error) {
	token := os.Getenv(tokenEnv)
	if token == "" {
		logger.Warnf("no CircleCI token in $%s: the CircleCI and release steps are skipped", tokenEnv)
		return nil, nil
	}
	client, err := circleciclient.New(circleciclient.Config{Token: token, Logger: logger, Transport: transport})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return client, nil
}

// Slug splits owner/name; a bare name takes defaultOwner, and without one
// the owner is required.
func Slug(arg, defaultOwner string) (owner, name string, err error) {
	parts := strings.SplitN(arg, "/", 2)
	switch {
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return parts[0], parts[1], nil
	case len(parts) == 1 && defaultOwner != "" && parts[0] != "":
		return defaultOwner, parts[0], nil
	}
	return "", "", microerror.Maskf(invalidArgError, "expected owner/repo, got %q", arg)
}

// ValidateOutput checks an --output value.
func ValidateOutput(output string) error {
	switch output {
	case OutputTable, OutputJSON:
		return nil
	}
	return microerror.Maskf(invalidFlagError, "--output %q: want %s or %s", output, OutputTable, OutputJSON)
}

// Report writes the result to w as a table or as JSON and returns an error
// when a step failed, so the command exits non-zero on a step that could
// not run to its end; drift and findings are the result's business.
func Report(w io.Writer, res *reconcile.Result, output string) error {
	switch output {
	case OutputJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return microerror.Mask(err)
		}
	default:
		if err := res.WriteTable(w); err != nil {
			return microerror.Mask(err)
		}
	}
	if failed := res.Failed(); len(failed) > 0 {
		names := make([]string, 0, len(failed))
		for _, s := range failed {
			names = append(names, string(s.Step)+": "+s.Summary)
		}
		return microerror.Maskf(stepFailedError, "%d step(s) failed: %s", len(failed), strings.Join(names, "; "))
	}
	return nil
}

// PipelineFiles reads the pipeline documents (reconcile.PipelineFiles,
// whatever is missing is skipped) from a .circleci directory and checks
// that each parses; the result is empty when the directory holds none.
func PipelineFiles(dir string) ([][]byte, error) {
	var files [][]byte
	for _, file := range reconcile.PipelineFiles {
		p := filepath.Join(dir, file)
		raw, err := os.ReadFile(filepath.Clean(p)) //nolint:gosec // the directory comes from the operator's own flag
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if _, err := reconcile.GateJobs(raw); err != nil {
			return nil, microerror.Maskf(invalidFlagError, "%s: %v", p, err)
		}
		files = append(files, raw)
	}
	return files, nil
}

// Schema is the repositories schema: the file at path when given, else the
// schema on giantswarm/github main (with client), else the embedded copy.
func Schema(ctx context.Context, logger *logrus.Logger, client *githubclient.Client, path string) (*reposetup.Schema, error) {
	if path != "" {
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, microerror.Mask(err)
		}
		return reposetup.CompileSchema(data, reposetup.SchemaOriginFile)
	}
	if client != nil {
		schema, err := reposetup.FetchSchema(ctx, client)
		if err == nil {
			return schema, nil
		}
		logger.Warnf("cannot read the repositories schema from %s/%s@%s (%v): validating against the embedded copy", reposetup.SchemaRepositoryOwner, reposetup.SchemaRepository, reposetup.SchemaRef, err)
	}
	return reposetup.EmbeddedSchema()
}

// LogWriter adapts the logger to the io.Writer the Runner and the Renderer
// log through: one debug line per write.
func LogWriter(logger *logrus.Logger) io.Writer {
	return logWriter{logger}
}

type logWriter struct{ logger *logrus.Logger }

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		w.logger.Debug(line)
	}
	return len(p), nil
}
