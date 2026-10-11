package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// callTimeout bounds the manager's dry run: the team file read as the
// person, their teams, and a name check on GitHub per entry.
const callTimeout = 2 * time.Minute

// The engines that answer.
const (
	engineManager = "manager"
	engineLocal   = "local"
)

// engine says who validated, under the output document's engine key:
// giantswarm-repo-manager through the person's muster endpoint, or devctl's
// own engine and why it ran.
type engine struct {
	Name string `json:"name"`
	// Server, Endpoint and Login are the manager's: its name in muster, the
	// endpoint and the account the muster token acts as.
	Server   string `json:"server,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Login    string `json:"login,omitempty"`
	// Version is the devctl whose embedded engine ran, Reason why it did:
	// --local, or CI.
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	// open is client.Open; tests inject a session over a fake.
	open client.Opener
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx))
}

func (r *runner) run(ctx context.Context) error {
	// The dry run is the command's output and stdout is for it alone; the
	// log lines devctl otherwise writes to stdout go to stderr here.
	r.logger.SetOutput(r.stderr)

	reason, err := r.flag.localReason()
	if err != nil {
		return microerror.Mask(err)
	}

	teamFile, err := reposetup.ReadTeamFile(r.flag.TeamFile)
	if err != nil {
		return microerror.Mask(err)
	}

	var who engine
	var payload json.RawMessage
	if reason == "" {
		who, payload, err = r.manager(ctx, teamFile)
	} else {
		who, payload, err = r.local(ctx, teamFile, reason)
	}
	if err != nil {
		return microerror.Mask(err)
	}

	if err := print(r.stdout, who, payload); err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(verdict(payload))
}

// manager asks giantswarm-repo-manager for the dry run of the entries as
// declarations being added to the team's file -- its engine and the schema
// on giantswarm/github main, the names checked by its App, the guard
// notices from the person's memberships as it reads them -- and returns
// its answer as it came. Without a usable muster token the error names the
// login and --local.
func (r *runner) manager(ctx context.Context, teamFile *reposetup.TeamFile) (engine, json.RawMessage, error) {
	session, err := r.open(ctx)
	if err != nil {
		return engine{}, nil, microerror.Mask(withLocalHint(err))
	}

	selected, err := reposetup.SelectEntries(teamFile, r.flag.Entries)
	if err != nil {
		return engine{}, nil, microerror.Mask(err)
	}
	entries := make([]any, 0, len(selected))
	for _, d := range selected {
		instance, err := d.Instance()
		if err != nil {
			return engine{}, nil, microerror.Mask(err)
		}
		entries = append(entries, instance)
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolValidateRepository, map[string]any{"team": teamFile.Team, "entries": entries})
	if err != nil {
		return engine{}, nil, microerror.Mask(withLocalHint(err))
	}

	return engine{Name: engineManager, Server: manager.Server, Endpoint: session.Endpoint, Login: session.Login}, payload, nil
}

// withLocalHint names --local beside the login a muster auth error asks
// for: devctl's own engine is the way without a sign-in.
func withLocalHint(err error) error {
	var required *authstore.AuthRequiredError
	if !errors.As(err, &required) {
		return err
	}
	return &authstore.AuthRequiredError{
		Identity: required.Identity,
		Cause:    required.Cause + "; --local runs devctl's own engine without a sign-in",
		Hint:     required.Hint,
	}
}

// local runs devctl's own engine: the schema (--schema, else the one on
// giantswarm/github main with a GitHub token, else the embedded copy) and
// the name checks with the GitHub token when there is one.
func (r *runner) local(ctx context.Context, teamFile *reposetup.TeamFile, reason string) (engine, json.RawMessage, error) {
	client, err := r.githubClient(ctx)
	if err != nil {
		return engine{}, nil, microerror.Mask(err)
	}

	schema, err := r.schema(ctx, client)
	if err != nil {
		return engine{}, nil, microerror.Mask(err)
	}

	validator := reposetup.Validator{
		Schema: schema,
		Owner:  r.flag.Owner,
	}
	if client != nil {
		validator.Names = reposetup.GitHubNameChecker{Repositories: client}
	}

	result, err := validator.Validate(ctx, reposetup.Request{
		TeamFile:    teamFile,
		Names:       r.flag.Entries,
		Mode:        r.flag.mode(),
		Author:      r.flag.Author,
		AuthorTeams: r.flag.AuthorTeams,
	})
	if err != nil {
		return engine{}, nil, microerror.Mask(err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return engine{}, nil, microerror.Mask(err)
	}

	return engine{Name: engineLocal, Version: project.Version(), Reason: reason}, payload, nil
}

// print writes the one document stdout carries: the engine beside every
// field of the answer as it came, the manager's or the local engine's.
func print(w io.Writer, who engine, payload json.RawMessage) error {
	doc := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return microerror.Maskf(client.UnexpectedAnswerError, "the validation's answer is not a JSON object: %v", err)
	}
	doc["engine"] = who

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return microerror.Mask(enc.Encode(doc))
}

// verdict is the exit status: refusedError when an entry is refused, the
// problems being in the document.
func verdict(payload json.RawMessage) error {
	var result reposetup.Result
	if err := json.Unmarshal(payload, &result); err != nil {
		return microerror.Maskf(client.UnexpectedAnswerError, "the validation's answer carries no verdict: %v", err)
	}
	if result.Accepted {
		return nil
	}
	refused := 0
	for _, entry := range result.Entries {
		if !entry.Accepted {
			refused++
		}
	}
	return microerror.Maskf(refusedError, "%d of %d entries refused; the problems name the fields", refused, len(result.Entries))
}

// githubClient is the local engine's client for the schema fetch and the
// name checks: the App login, or the token in --github-token-envvar (by
// default $DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN or $OPSCTL_GITHUB_TOKEN)
// overriding it. nil when neither is there: the data read is public, so no
// token is not an error.
func (r *runner) githubClient(ctx context.Context) (*githubclient.Client, error) {
	var envVars []string
	if r.flag.GithubTokenEnvVar != "" {
		envVars = []string{r.flag.GithubTokenEnvVar}
	}
	token, err := authstore.ResolveGitHub(ctx, envVars...)
	var required *authstore.AuthRequiredError
	if errors.As(err, &required) {
		r.logger.Warnf("no GitHub token (%s): validating against the embedded schema, repository names are not checked", required.Cause)
		return nil, nil
	} else if err != nil {
		return nil, microerror.Mask(err)
	}
	token.WarnOnce(r.stderr)

	client, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: token.Value,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return client, nil
}

// schema is --schema when given, else the schema on giantswarm/github main,
// else the embedded copy.
func (r *runner) schema(ctx context.Context, client *githubclient.Client) (*reposetup.Schema, error) {
	if r.flag.Schema != "" {
		data, err := os.ReadFile(filepath.Clean(r.flag.Schema))
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
		r.logger.Warnf("cannot read the repositories schema from %s/%s@%s (%v): validating against the embedded copy", reposetup.SchemaRepositoryOwner, reposetup.SchemaRepository, reposetup.SchemaRef, err)
	}

	return reposetup.EmbeddedSchema()
}
