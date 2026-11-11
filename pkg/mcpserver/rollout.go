package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

// ToolRolloutWait is the rollout wait's name.
const ToolRolloutWait = "rollout_wait"

func rolloutWaitTool(noProgress time.Duration, installations []string) mcp.Tool {
	return mcp.NewTool(ToolRolloutWait,
		mcp.WithDescription("Wait until a release runs on an installation, in one call: devctl rollout wait. "+
			"First its images and charts are pullable and its tag pipeline green, then every Flux HelmRelease and App CR of its charts on the installation's management cluster runs the version, Ready, its workloads rolled out. "+
			"The cluster is read as you, through your token exchanged at the installation's Dex: it answers by your access. "+
			"Name exactly one of version and pr; a pr that releases nothing (a configuration change) is followed as its merge commit. "+
			progressDescription(noProgress)+
			"Answers devctl's document: verdict rolled_out, rollout_failed, timeout, not_applicable, no_release, rollout_unknown or auth_required, and its exitCode."),
		mcp.WithString("installation", mcp.Required(), mcp.Description("The installation, one of: "+orNone(installations)+".")),
		mcp.WithString("repository", mcp.Required(), mcp.Description("The repository, owner/repo.")),
		mcp.WithString("version", mcp.Description("The release's version, vX.Y.Z or X.Y.Z.")),
		mcp.WithInteger("pr", mcp.Description("The merged pull request whose release to wait for.")),
		mcp.WithString("timeout", mcp.Description("How long the release wait and the rollout may take together, a Go duration (30m, 1h); at most 55m. Default 30m.")),
		mcp.WithArray("images", mcp.WithStringItems(), mcp.Description("Images to wait for besides the ones the repository declares.")),
		mcp.WithArray("charts", mcp.WithStringItems(), mcp.Description("Charts to wait for besides the ones the repository declares, e.g. one a hand-written pipeline pushes.")),
		mcp.WithArray("helmreleases", mcp.WithStringItems(), mcp.Description("HelmReleases to wait for whatever chart they deploy, <name> or <namespace>/<name>.")),
		readOnly(),
	)
}

func (s *Server) rolloutWait(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	installation := request.GetString("installation", "")
	doc := rolloutwait.Document{Envelope: agentcli.NewEnvelope(rolloutwait.Command, time.Now()), Result: rolloutwait.NewResult(installation, installation)}
	doc.Release.Result = releasewait.NewResult(request.GetString("repository", ""))
	err := s.runRolloutWait(ctx, request, installation, &doc)
	return answer(&doc, rolloutwait.VerdictRolledOut, err)
}

func (s *Server) runRolloutWait(ctx context.Context, request mcp.CallToolRequest, installation string, doc *rolloutwait.Document) error {
	owner, repo, err := repository(request)
	if err != nil {
		return err
	}
	if !installationName.MatchString(installation) {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not an installation name", installation)
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
	timeout, err := timeoutOf(request, rolloutwait.DefaultTimeout)
	if err != nil {
		return err
	}
	token := progressToken(request)
	timeout = s.bound(token, ToolRolloutWait, timeout, doc.Warn)
	githubToken, err := s.credentials.GitHubToken(ctx)
	if err != nil {
		return err
	}
	what := fmt.Sprintf("%s/%s %s on %s", owner, repo, version, installation)
	if version == "" {
		what = fmt.Sprintf("the release of %s/%s#%d on %s", owner, repo, pr, installation)
	}
	return s.keepAlive(ctx, token, what, func(progress *agentcli.Progress) error {
		deadline := s.clock.Now().Add(timeout)
		retrying := func(base http.RoundTripper) http.RoundTripper {
			return &agentcli.Retrying{Base: base, Clock: s.clock, Progress: progress, Warn: doc.Warn}
		}
		// The installation is opened before the release wait, so a refusal
		// ends the call at once rather than after the release.
		client, err := s.clusters.Open(ctx, installation, retrying)
		if err != nil {
			return err
		}
		sources, err := s.sources.ReleaseWait(ctx, githubToken, retrying(nil))
		if err != nil {
			return err
		}
		return rolloutwait.Release{
			Installation:   installation,
			KubeContext:    installation,
			Owner:          owner,
			Repo:           repo,
			Version:        version,
			PR:             pr,
			Timeout:        timeout,
			ReleaseTimeout: timeout,
			Deadline:       deadline,
			Images:         request.GetStringSlice("images", nil),
			Charts:         request.GetStringSlice("charts", nil),
			HelmReleases:   request.GetStringSlice("helmreleases", nil),
			// The server holds no CircleCI credential.
			CircleCIFromStatuses: true,
			SignIn:               "sign in to muster again, then call again",
			Sources:              sources,
			Client:               client,
			Endpoints:            s.endpoints,
			Clock:                s.clock,
			Progress:             progress,
			Warn:                 doc.Warn,
		}.Wait(ctx, doc)
	})
}
