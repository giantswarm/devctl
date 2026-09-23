package wait

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// clients are the sources one wait reads, opened after the gate.
type clients struct {
	github   releasewait.GitHub
	entries  releasewait.EntryFinder
	circleci func(ctx context.Context) (releasewait.CircleCI, error)
	registry releasewait.Prober
	catalog  releasewait.CatalogReader
	rate     func() githubclient.RateLimit
}

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// open is openClients; tests inject clients over mocks.
	open func(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*clients, error)
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	return r.run(cmd.Context(), args)
}

func (r *runner) run(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	repository := ""
	if len(args) > 0 {
		repository = args[0]
	}
	doc := releasewait.Document{Envelope: agentcli.NewEnvelope(releasewait.Command, time.Now()), Result: releasewait.NewResult(repository)}
	err := r.wait(ctx, args, &doc)
	doc.Finish(time.Now(), agentcli.VerdictAvailable, err)
	if err := agentcli.Emit(r.stdout, doc); err != nil {
		return err
	}
	return doc.Err()
}

func (r *runner) wait(ctx context.Context, args []string, doc *releasewait.Document) error {
	owner, repo, ok := strings.Cut(args[0], "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not a repository: expected owner/repo", args[0])
	}
	version := ""
	if len(args) > 1 {
		version = args[1]
	}
	if (version == "") == (r.flag.PR == 0) {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "pass exactly one of a version (vX.Y.Z or X.Y.Z) and --pr <number>")
	}
	if version != "" {
		if _, err := releasewait.ParseVersion(version); err != nil {
			return err
		}
	}

	clock, err := agentcli.SystemClock()
	if err != nil {
		return err
	}
	endpoints := agentcli.EndpointsFromEnv()
	progress := agentcli.NewProgress(r.stderr, r.flag.Progress)

	// A read that fails in transit or with a 5xx is sent again, not taken
	// as the wait's outcome.
	retrying := &agentcli.Retrying{Clock: clock, Progress: progress, Warn: doc.Warn}
	progress.Printf("reading the GitHub token from the keychain")
	c, err := r.open(ctx, endpoints, retrying, doc.Warn)
	if err != nil {
		return err
	}

	waiter, err := releasewait.New(releasewait.Config{
		Owner:        owner,
		Repo:         repo,
		Version:      version,
		PR:           r.flag.PR,
		Timeout:      r.flag.Timeout,
		Catalog:      r.flag.Catalog,
		GitHub:       c.github,
		Entries:      c.entries,
		CircleCI:     c.circleci,
		Registry:     c.registry,
		CatalogIndex: c.catalog,
		Endpoints:    endpoints,
		Clock:        clock,
		Rate:         c.rate,
		Progress:     progress,
		Warn:         doc.Warn,
	})
	if err != nil {
		return err
	}
	return waiter.Wait(ctx, &doc.Result)
}

// openClients is the production wiring: the GitHub token through the gate,
// conditional requests below it, the team files of the organisation, the
// CircleCI token through its gate when the tag turns out to carry a
// CircleCI configuration, the registries and the catalog index. The GitHub
// and CircleCI requests go through transport.
func openClients(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*clients, error) {
	token, err := authstore.RequireGitHub(ctx)
	if err != nil {
		return nil, err
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	gh, conditional, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: token.Value, BaseURL: endpoints.GitHubAPIURL, Transport: transport})
	if err != nil {
		return nil, err
	}
	return &clients{
		github:  gh,
		entries: releasewait.TeamFileEntries{GitHub: gh.GetUnderlyingClient(ctx)},
		circleci: func(ctx context.Context) (releasewait.CircleCI, error) {
			token, err := authstore.RequireCircleCI(ctx)
			if err != nil {
				return nil, err
			}
			warn(token.Warning)
			client, err := circleciclient.New(circleciclient.Config{Token: token.Value, BaseURL: circleciclient.BaseURLFromAPIURL(endpoints.CircleCIAPIURL), HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				return nil, err
			}
			return client, nil
		},
		registry: releasewait.RegistryProber{Endpoints: endpoints},
		catalog:  releasewait.CatalogIndex{BaseURL: releasewait.CatalogURLFromEnv()},
		rate:     conditional.Rate,
	}, nil
}
