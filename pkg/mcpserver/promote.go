package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

// ToolReleasePromote is the promotion tool's name, served with
// [Config.ReleasePromote] only.
const ToolReleasePromote = "release_promote"

func releasePromoteTool() mcp.Tool {
	return mcp.NewTool(ToolReleasePromote,
		mcp.WithDescription("Promote the latest release candidate of auto-release repositories to a stable release, as you, in one call: devctl release promote. "+
			"Name exactly one of repositories and team. Dispatches each repository's Auto Release workflow with release-type stable and follows the runs until the stable gate and the tag are done. "+
			"Call it with a progressToken; it reports progress until its verdict. "+
			"Answers devctl's document: one entry per repository with its state, verdict green when every repository is dispatched, would be or has nothing to promote, red naming the others, and its exitCode."),
		mcp.WithArray("repositories", mcp.WithStringItems(), mcp.Description("The repositories, owner/repo each.")),
		mcp.WithString("team", mcp.Description("Promote every auto-release repository of this team's file in giantswarm/github (team-bumblebee) instead.")),
		mcp.WithBoolean("dryRun", mcp.Description("Pick and check the candidates, dispatch nothing.")),
		mcp.WithString("version", mcp.Description("Promote this candidate (vX.Y.Z-rc.N) of the one named repository instead of the latest.")),
		mcp.WithString("branch", mcp.Description("Promote the candidate of this branch of the one named repository (a fork line's release-X.Y).")),
		mcp.WithBoolean("noFollow", mcp.Description("Return once dispatched instead of following the runs.")),
		mcp.WithString("followTimeout", mcp.Description("How long to follow the runs, a Go duration; at most 55m. Default 20m.")),
		writes(false),
	)
}

func (s *Server) releasePromote(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dryRun := request.GetBool("dryRun", false)
	result := releasepromote.NewResult(request.GetString("team", ""))
	result.DryRun = dryRun
	doc := releasepromote.Document{Envelope: agentcli.NewEnvelope(releasepromote.Command, time.Now()), Result: result}
	err := s.runReleasePromote(ctx, request, &doc)
	return answer(&doc, agentcli.VerdictGreen, err)
}

func (s *Server) runReleasePromote(ctx context.Context, request mcp.CallToolRequest, doc *releasepromote.Document) error {
	followTimeout := releasepromote.DefaultFollowTimeout
	if arg := request.GetString("followTimeout", ""); arg != "" {
		var err error
		followTimeout, err = time.ParseDuration(arg)
		if err != nil || followTimeout < 0 || followTimeout > MaxTimeout {
			return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "followTimeout must be a duration of at most %s, got %q", MaxTimeout, arg)
		}
	}
	call := releasepromote.Call{
		Repositories:  request.GetStringSlice("repositories", nil),
		Team:          request.GetString("team", ""),
		DryRun:        doc.DryRun,
		Version:       request.GetString("version", ""),
		Branch:        request.GetString("branch", ""),
		Follow:        !request.GetBool("noFollow", false),
		FollowTimeout: followTimeout,
	}
	if err := call.Check(); err != nil {
		return err
	}
	token := progressToken(request)
	if token == nil {
		return errNoProgressToken(ToolReleasePromote)
	}
	githubToken, err := s.credentials.GitHubToken(ctx)
	if err != nil {
		return err
	}
	what := "the promotion of " + call.Team
	if call.Team == "" {
		what = "the promotion of " + strings.Join(call.Repositories, ", ")
	}
	return s.keepAlive(ctx, token, what, func(progress *agentcli.Progress) error {
		// A read that fails in transit or with a 5xx is sent again; a
		// dispatch is never repeated.
		retrying := &agentcli.Retrying{Clock: s.clock, Progress: progress, Warn: doc.Warn}
		sources, err := s.sources.ReleasePromote(ctx, githubToken, retrying)
		if err != nil {
			return err
		}
		return call.Run(ctx, sources, s.clock, progress, doc)
	})
}
