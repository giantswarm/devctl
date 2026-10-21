package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// The tools' names.
const (
	ToolPRWait        = "pr_wait"
	ToolPRMergeStatus = "pr_merge_status"
	ToolReleaseWait   = "release_wait"
)

const timeoutDescription = "How long the wait may take, a Go duration (30m, 1h); at most 55m. Default 30m."

func prWaitTool() mcp.Tool {
	return mcp.NewTool(ToolPRWait,
		mcp.WithDescription("Wait until a pull request's head is green, red, or cannot become green as it is, in one call: devctl pr wait. "+
			"Call it with a progressToken; it reports progress until its verdict. "+
			"Answers devctl's document: verdict green, red, timeout, not_applicable, required_missing, approval_required, no_checks or auth_required, and its exitCode."),
		mcp.WithString("repository", mcp.Required(), mcp.Description("The repository, owner/repo.")),
		mcp.WithInteger("number", mcp.Required(), mcp.Description("The pull request's number.")),
		mcp.WithString("timeout", mcp.Description(timeoutDescription)),
		readOnly(),
	)
}

func (s *Server) prWait(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := prwait.NewDocument(time.Now())
	err := s.runPRWait(ctx, request, &doc)
	return answer(&doc, agentcli.VerdictGreen, err)
}

func (s *Server) runPRWait(ctx context.Context, request mcp.CallToolRequest, doc *prwait.Document) error {
	owner, repo, err := repository(request)
	if err != nil {
		return err
	}
	number := request.GetInt("number", 0)
	doc.Repository, doc.Number = owner+"/"+repo, number
	if number <= 0 {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "number must be a pull request's number, got %d", number)
	}
	timeout, err := timeoutOf(request, prwait.DefaultTimeout)
	if err != nil {
		return err
	}
	token := progressToken(request)
	if token == nil {
		return errNoProgressToken(ToolPRWait)
	}
	githubToken, err := s.credentials.GitHubToken(ctx)
	if err != nil {
		return err
	}
	doc.Identity = "app"
	return s.keepAlive(ctx, token, fmt.Sprintf("%s/%s#%d", owner, repo, number), func(progress *agentcli.Progress) error {
		retrying := &agentcli.Retrying{Clock: s.clock, Progress: progress, Warn: doc.Warn}
		sources, err := s.sources.PRWait(ctx, githubToken, retrying)
		if err != nil {
			return err
		}
		waiter, err := prwait.New(prwait.Config{
			GitHub:  sources.GitHub,
			Rate:    sources.Rate,
			Entries: sources.Entries,
			// The server holds no CircleCI credential.
			CircleCIFromStatuses: true,
			Clock:                s.clock,
			Progress:             progress,
			Timeout:              timeout,
		})
		if err != nil {
			return err
		}
		result, err := waiter.Wait(ctx, owner, repo, number)
		doc.Take(result)
		return err
	})
}

func prMergeStatusTool() mcp.Tool {
	return mcp.NewTool(ToolPRMergeStatus,
		mcp.WithDescription("Read a merge this devctl server runs, by its handle, or list them without one: devctl pr merge status. "+
			"Answers devctl's document: the merge's own exit code and verdict once it ended, verdict running while it runs."),
		mcp.WithString("handle", mcp.Description("The merge's handle; empty lists every merge.")),
		readOnly(),
	)
}

func (s *Server) prMergeStatus(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := mergejob.NewStatusDocument(time.Now())
	ok, err := mergejob.ReadStatus(s.merges, request.GetString("handle", ""), &doc)
	return answer(&doc, ok, err)
}

func releaseWaitTool() mcp.Tool {
	return mcp.NewTool(ToolReleaseWait,
		mcp.WithDescription("Wait until a release's images and charts are pullable and its tag pipeline is green, in one call: devctl release wait. "+
			"Name exactly one of version and pr. Call it with a progressToken; it reports progress until its verdict. "+
			"Answers devctl's document: verdict available, ci_failed, timeout, not_applicable, no_release, ambiguous_release, status_failed or auth_required, and its exitCode."),
		mcp.WithString("repository", mcp.Required(), mcp.Description("The repository, owner/repo.")),
		mcp.WithString("version", mcp.Description("The release's version, vX.Y.Z or X.Y.Z.")),
		mcp.WithInteger("pr", mcp.Description("The merged pull request whose release to wait for.")),
		mcp.WithString("timeout", mcp.Description(timeoutDescription)),
		mcp.WithBoolean("catalog", mcp.Description("Also wait for the chart in its catalog's index.")),
		mcp.WithArray("images", mcp.WithStringItems(), mcp.Description("Images to wait for besides the ones the repository declares.")),
		mcp.WithArray("charts", mcp.WithStringItems(), mcp.Description("Charts to wait for besides the ones the repository declares.")),
		readOnly(),
	)
}

func (s *Server) releaseWait(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := releasewait.Document{
		Envelope: agentcli.NewEnvelope(releasewait.Command, time.Now()),
		Result:   releasewait.NewResult(request.GetString("repository", "")),
	}
	err := s.runReleaseWait(ctx, request, &doc)
	return answer(&doc, agentcli.VerdictAvailable, err)
}

func (s *Server) runReleaseWait(ctx context.Context, request mcp.CallToolRequest, doc *releasewait.Document) error {
	owner, repo, err := repository(request)
	if err != nil {
		return err
	}
	version, pr := request.GetString("version", ""), request.GetInt("pr", 0)
	if (version == "") == (pr == 0) {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "name exactly one of version (vX.Y.Z or X.Y.Z) and pr")
	}
	if version != "" {
		if _, err := releasewait.ParseVersion(version); err != nil {
			return err
		}
	}
	timeout, err := timeoutOf(request, releasewait.DefaultTimeout)
	if err != nil {
		return err
	}
	token := progressToken(request)
	if token == nil {
		return errNoProgressToken(ToolReleaseWait)
	}
	githubToken, err := s.credentials.GitHubToken(ctx)
	if err != nil {
		return err
	}
	what := fmt.Sprintf("the release of %s/%s %s", owner, repo, version)
	if version == "" {
		what = fmt.Sprintf("the release of %s/%s#%d", owner, repo, pr)
	}
	return s.keepAlive(ctx, token, what, func(progress *agentcli.Progress) error {
		retrying := &agentcli.Retrying{Clock: s.clock, Progress: progress, Warn: doc.Warn}
		sources, err := s.sources.ReleaseWait(ctx, githubToken, retrying)
		if err != nil {
			return err
		}
		waiter, err := releasewait.New(releasewait.Config{
			Owner:   owner,
			Repo:    repo,
			Version: version,
			PR:      pr,
			Timeout: timeout,
			Catalog: request.GetBool("catalog", false),
			Images:  request.GetStringSlice("images", nil),
			Charts:  request.GetStringSlice("charts", nil),
			GitHub:  sources.GitHub,
			Entries: sources.Entries,
			// The server holds no CircleCI credential.
			CircleCIFromStatuses: true,
			Registry:             sources.Registry,
			CatalogIndex:         sources.CatalogIndex,
			Endpoints:            s.endpoints,
			Clock:                s.clock,
			Rate:                 sources.Rate,
			Progress:             progress,
			Warn:                 doc.Warn,
		})
		if err != nil {
			return err
		}
		return waiter.Wait(ctx, &doc.Result)
	})
}

// repository is the call's owner/repo.
func repository(request mcp.CallToolRequest) (string, string, error) {
	arg := request.GetString("repository", "")
	owner, repo, ok := strings.Cut(arg, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not a repository: expected owner/repo", arg)
	}
	return owner, repo, nil
}

// timeoutOf is the call's timeout, fallback without one, at most MaxTimeout.
func timeoutOf(request mcp.CallToolRequest, fallback time.Duration) (time.Duration, error) {
	arg := request.GetString("timeout", "")
	if arg == "" {
		return fallback, nil
	}
	timeout, err := time.ParseDuration(arg)
	if err != nil || timeout <= 0 || timeout > MaxTimeout {
		return 0, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "timeout must be a duration above 0 and at most %s, got %q", MaxTimeout, arg)
	}
	return timeout, nil
}
