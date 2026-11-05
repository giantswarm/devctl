package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/internal/validate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// The reservation tools' names: devctl reservation reserve, list, extend and
// release, as the person. reap stays the CI bot's.
const (
	ToolReservationReserve = "reservation_reserve"
	ToolReservationList    = "reservation_list"
	ToolReservationExtend  = "reservation_extend"
	ToolReservationRelease = "reservation_release"
)

const (
	reservationRepositoryDescription = "The GitOps repository holding the management clusters, owner/repo. Default " + reservation.DefaultGitOpsRepository + "."
	reservationDryRunDescription     = "Make the commits in the call's checkout and answer them as patch; push nothing."
	reservationPullRequestDesc       = "The holder's pull request, owner/repo#number."
	reservationCheckoutDescription   = "Each call works on a fresh shallow clone of the GitOps repository's main, removed when it ends, and pushes to main as you: your GitHub login is the holder and your noreply address the commits' author. "
)

// reservationHead is what every reservation tool's document carries besides
// its command's own result.
type reservationHead struct {
	agentcli.Envelope
	// Repository is the GitOps repository, owner/repo.
	Repository string `json:"repository"`
	// User is the person who called, the holder of what they reserve.
	User string `json:"user,omitempty"`
	// DryRun says nothing was pushed; Patch is what would have been.
	DryRun bool   `json:"dryRun,omitempty"`
	Patch  string `json:"patch,omitempty"`
}

func newReservationHead(command string, request mcp.CallToolRequest) reservationHead {
	return reservationHead{
		Envelope:   agentcli.NewEnvelope(command, time.Now()),
		Repository: request.GetString("repository", reservation.DefaultGitOpsRepository),
		DryRun:     request.GetBool("dryRun", false),
	}
}

// inCheckout runs work, as the caller, in a clone of head's repository of its
// own, removed when work returns. For a dry run it answers the commits work
// made as head's patch.
func (s *Server) inCheckout(ctx context.Context, request mcp.CallToolRequest, head *reservationHead, what string, work func(dir, user string) error) error {
	owner, _, err := ownerRepo(head.Repository)
	if err != nil {
		return err
	}
	call := callFrom(ctx)
	call.owner = owner
	token, err := s.credentials.GitHubToken(ctx)
	if err != nil {
		return err
	}
	if call.caller.Login == "" {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "this devctl server names no caller from your grant, so it has no holder for a reservation")
	}
	head.User = call.caller.Login
	return s.keepAlive(ctx, progressToken(request), what, func(*agentcli.Progress) error {
		dir, remove, err := s.checkouts.Clone(ctx, head.Repository, token, call.caller)
		if err != nil {
			return err
		}
		defer remove()
		err = work(dir, head.User)
		if head.DryRun {
			head.Patch, _ = gitIn(ctx, dir, "log", "-p", "--reverse", "origin/main..HEAD")
		}
		return reservationOutcome(err)
	})
}

// ownerRepo splits a GitOps repository, owner/repo.
func ownerRepo(repository string) (string, string, error) {
	if !repositoryPattern.MatchString(repository) {
		return "", "", agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not a repository: expected owner/repo", repository)
	}
	owner, repo, _ := strings.Cut(repository, "/")
	return owner, repo, nil
}

// reservationOutcome places an engine error in the exit-code table: a
// reservation another holder has is refused, as is a push the GitOps
// repository did not take; a cluster, app or reservation that is not there
// is not_applicable; a wrong argument is usage. Of joined errors the first
// placed one decides, and the reason names them all.
func reservationOutcome(err error) error {
	if err == nil {
		return nil
	}
	var coder agentcli.ExitCoder
	if errors.As(err, &coder) {
		return err
	}
	parts := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts = joined.Unwrap()
	}
	for _, part := range parts {
		if code, verdict, ok := reservationCode(part); ok {
			return agentcli.NewExitError(code, verdict, "%s", err.Error())
		}
	}
	return err
}

func reservationCode(err error) (int, agentcli.Verdict, bool) {
	switch {
	case reservation.IsAlreadyReserved(err), reservation.IsClusterLocked(err):
		return agentcli.ExitRefused, agentcli.VerdictRefused, true
	case reservation.IsPushFailed(err), reservation.IsPush(err):
		return agentcli.ExitRefused, agentcli.VerdictRefused, true
	case reservation.IsClusterNotFound(err), reservation.IsClusterNotEnabled(err),
		reservation.IsAppNotFound(err), reservation.IsAppAmbiguous(err), reservation.IsAppNotSupported(err),
		reservation.IsNotReserved(err), reservation.IsNothingToExtend(err):
		return agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, true
	case reservation.IsInvalidConfig(err), reservation.IsInvalidDuration(err):
		return agentcli.ExitUsage, agentcli.VerdictUsage, true
	}
	return 0, "", false
}

func usage(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, format, args...)
}

// checkCluster refuses a cluster that is not a name: it becomes a path.
func checkCluster(cluster string) error {
	if err := validate.Name("cluster", cluster); err != nil {
		return usage("%v", err)
	}
	return nil
}

// checkPullRequest refuses a pull request that is not owner/repo#number;
// empty is none.
func checkPullRequest(pullRequest string) error {
	if pullRequest == "" {
		return nil
	}
	if _, _, _, err := reservation.SplitPullRequest(pullRequest); err != nil {
		return usage("pullRequest %q is not owner/repo#number", pullRequest)
	}
	return nil
}

// branchPattern is a branch name as the reservation writes it into YAML and
// the commit message: no space, quote or control character.
var branchPattern = regexp.MustCompile(`^[A-Za-z0-9._/+-]{1,255}$`)

func reservationReserveTool() mcp.Tool {
	return mcp.NewTool(ToolReservationReserve,
		mcp.WithDescription("Reserve one collection app on one management cluster for the dev builds of a branch, as you, in one call: devctl reservation reserve. "+
			reservationCheckoutDescription+
			"Repeating your own reservation refreshes its window; another holder's is refused naming them, the branch and its end. Flux applies the commit in a few minutes. "+
			"Answers devctl's document: the reservation (app, commit, sourceName, semverFilter, from, until), verdict green, refused (held by another, or the push was not taken), not_applicable or usage, and its exitCode."),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("The management cluster, graveler.")),
		mcp.WithString("app", mcp.Required(), mcp.Description("The collection app, by the chart its source serves.")),
		mcp.WithString("branch", mcp.Required(), mcp.Description("The app repository's branch whose dev builds the cluster follows.")),
		mcp.WithString("pullRequest", mcp.Description(reservationPullRequestDesc+" Without one, extend and release by pull request do not find the reservation.")),
		mcp.WithString("duration", mcp.Description("How long it lasts: "+reservation.AcceptedDurations+". Default 10h, at most the cluster's maximum.")),
		mcp.WithBoolean("exclusive", mcp.Description("Lock the whole cluster, not only the app.")),
		mcp.WithString("repository", mcp.Description(reservationRepositoryDescription)),
		mcp.WithBoolean("dryRun", mcp.Description(reservationDryRunDescription)),
		writes(false),
	)
}

// reserveDocument is reservation_reserve's document.
type reserveDocument struct {
	reservationHead
	Cluster     string `json:"cluster"`
	Branch      string `json:"branch"`
	PullRequest string `json:"pullRequest,omitempty"`
	Scope       string `json:"scope"`
	*reservation.Result
}

func (s *Server) reservationReserve(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := reserveDocument{
		reservationHead: newReservationHead("reservation reserve", request),
		Cluster:         request.GetString("cluster", ""),
		Branch:          request.GetString("branch", ""),
		PullRequest:     request.GetString("pullRequest", ""),
		Scope:           reservation.ScopeApp,
	}
	if request.GetBool("exclusive", false) {
		doc.Scope = reservation.ScopeExclusive
	}
	err := s.runReserve(ctx, request, &doc)
	return answer(&doc, agentcli.VerdictGreen, err)
}

func (s *Server) runReserve(ctx context.Context, request mcp.CallToolRequest, doc *reserveDocument) error {
	app := request.GetString("app", "")
	if err := checkCluster(doc.Cluster); err != nil {
		return err
	}
	if err := validate.Name("app", app); err != nil {
		return usage("%v", err)
	}
	if !branchPattern.MatchString(doc.Branch) {
		return usage("branch %q is not a branch name", doc.Branch)
	}
	if err := checkPullRequest(doc.PullRequest); err != nil {
		return err
	}
	var duration time.Duration
	if arg := request.GetString("duration", ""); arg != "" {
		var err error
		if duration, err = reservation.ParseDuration(arg); err != nil {
			return usage("duration %q is not a duration. Accepted forms are %s", arg, reservation.AcceptedDurations)
		}
	}
	what := fmt.Sprintf("the reservation of %s on %s", app, doc.Cluster)
	return s.inCheckout(ctx, request, &doc.reservationHead, what, func(dir, user string) error {
		var result reservation.Result
		err := reservation.PushWithRetry(ctx, dir, doc.DryRun, func() error {
			var err error
			result, err = reservation.Reserve(reservation.Request{
				RepoDir:     dir,
				Cluster:     doc.Cluster,
				App:         app,
				Branch:      doc.Branch,
				User:        user,
				PullRequest: doc.PullRequest,
				Duration:    duration,
				Scope:       doc.Scope,
			})
			return err
		})
		if err == nil {
			doc.Result = &result
		}
		return err
	})
}

func reservationListTool() mcp.Tool {
	return mcp.NewTool(ToolReservationList,
		mcp.WithDescription("List the reservations of a management cluster, as you, in one call: devctl reservation list. "+
			"Reads a fresh shallow clone of the GitOps repository's main, removed when it ends. "+
			"Answers devctl's document: reservations, each with app, user (the holder), branch, pullRequest, scope, from, until and status active or expired, verdict green, not_applicable or usage, and its exitCode."),
		mcp.WithString("cluster", mcp.Required(), mcp.Description("The management cluster, graveler.")),
		mcp.WithString("repository", mcp.Description(reservationRepositoryDescription)),
		readOnly(),
	)
}

// listEntry is one reservation of reservation_list, as the CLI's JSON
// prints it.
type listEntry struct {
	reservation.Reservation
	Status string `json:"status"`
}

// listDocument is reservation_list's document.
type listDocument struct {
	reservationHead
	Cluster      string      `json:"cluster"`
	Reservations []listEntry `json:"reservations"`
}

func (s *Server) reservationList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := listDocument{
		reservationHead: newReservationHead("reservation list", request),
		Cluster:         request.GetString("cluster", ""),
		Reservations:    []listEntry{},
	}
	err := s.runList(ctx, request, &doc)
	return answer(&doc, agentcli.VerdictGreen, err)
}

func (s *Server) runList(ctx context.Context, request mcp.CallToolRequest, doc *listDocument) error {
	if err := checkCluster(doc.Cluster); err != nil {
		return err
	}
	return s.inCheckout(ctx, request, &doc.reservationHead, "the reservations of "+doc.Cluster, func(dir, _ string) error {
		all, err := reservation.List(reservation.ListRequest{RepoDir: dir, Cluster: doc.Cluster})
		if err != nil {
			return err
		}
		// The engine's windows are wall-clock time.
		now := time.Now()
		for _, r := range all {
			status := "active"
			if r.Expired(now) {
				status = "expired"
			}
			doc.Reservations = append(doc.Reservations, listEntry{r, status})
		}
		return nil
	})
}

func reservationExtendTool() mcp.Tool {
	return mcp.NewTool(ToolReservationExtend,
		mcp.WithDescription("Reset the expiry of every reservation a pull request holds across the enabled management clusters, and make you their holder, in one call: devctl reservation extend. "+
			reservationCheckoutDescription+
			"Answers devctl's document: extended, each with cluster, app, from and until (what landed, even when another failed), verdict green, refused, not_applicable (nothing to extend) or usage, and its exitCode."),
		mcp.WithString("pullRequest", mcp.Required(), mcp.Description(reservationPullRequestDesc)),
		mcp.WithString("repository", mcp.Description(reservationRepositoryDescription)),
		mcp.WithBoolean("dryRun", mcp.Description(reservationDryRunDescription)),
		writes(false),
	)
}

// extendDocument is reservation_extend's document.
type extendDocument struct {
	reservationHead
	PullRequest string                 `json:"pullRequest"`
	Extended    []reservation.Extended `json:"extended"`
}

func (s *Server) reservationExtend(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := extendDocument{
		reservationHead: newReservationHead("reservation extend", request),
		PullRequest:     request.GetString("pullRequest", ""),
		Extended:        []reservation.Extended{},
	}
	err := s.runExtend(ctx, request, &doc)
	return answer(&doc, agentcli.VerdictGreen, err)
}

func (s *Server) runExtend(ctx context.Context, request mcp.CallToolRequest, doc *extendDocument) error {
	if doc.PullRequest == "" {
		return usage("pullRequest must not be empty")
	}
	if err := checkPullRequest(doc.PullRequest); err != nil {
		return err
	}
	return s.inCheckout(ctx, request, &doc.reservationHead, "the extension of "+doc.PullRequest, func(dir, user string) error {
		extended, err := reservation.Extend(ctx, reservation.ExtendRequest{
			RepoDir:     dir,
			PullRequest: doc.PullRequest,
			User:        user,
			DryRun:      doc.DryRun,
		})
		doc.Extended = append(doc.Extended, extended...)
		return err
	})
}

func reservationReleaseTool() mcp.Tool {
	return mcp.NewTool(ToolReservationRelease,
		mcp.WithDescription("Return a reserved app to its release, as you, in one call: devctl reservation release. "+
			"Name cluster and app to release that app on that cluster (with pullRequest, only if that pull request holds it), or pullRequest alone to release everything it holds on every enabled cluster. "+
			reservationCheckoutDescription+
			"Answers devctl's document: cluster, app and commit, or released (one entry per release, what landed even when another failed), verdict green, refused, not_applicable (nothing reserved) or usage, and its exitCode."),
		mcp.WithString("cluster", mcp.Description("The management cluster, graveler; with app.")),
		mcp.WithString("app", mcp.Description("The reserved collection app; with cluster.")),
		mcp.WithString("pullRequest", mcp.Description(reservationPullRequestDesc)),
		mcp.WithString("repository", mcp.Description(reservationRepositoryDescription)),
		mcp.WithBoolean("dryRun", mcp.Description(reservationDryRunDescription)),
		writes(true),
	)
}

// releaseDocument is reservation_release's document.
type releaseDocument struct {
	reservationHead
	Cluster     string `json:"cluster,omitempty"`
	PullRequest string `json:"pullRequest,omitempty"`
	*reservation.ReleaseResult
	Released []reservation.Released `json:"released,omitempty"`
}

func (s *Server) reservationRelease(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc := releaseDocument{
		reservationHead: newReservationHead("reservation release", request),
		Cluster:         request.GetString("cluster", ""),
		PullRequest:     request.GetString("pullRequest", ""),
	}
	err := s.runRelease(ctx, request, &doc)
	return answer(&doc, agentcli.VerdictGreen, err)
}

func (s *Server) runRelease(ctx context.Context, request mcp.CallToolRequest, doc *releaseDocument) error {
	app := request.GetString("app", "")
	if err := checkPullRequest(doc.PullRequest); err != nil {
		return err
	}
	if doc.Cluster == "" {
		if doc.PullRequest == "" || app != "" {
			return usage("name cluster and app, or pullRequest alone to release everything it holds")
		}
		doc.Released = []reservation.Released{}
		return s.inCheckout(ctx, request, &doc.reservationHead, "the release of "+doc.PullRequest, func(dir, user string) error {
			released, err := reservation.ReleaseAll(ctx, reservation.ReleaseAllRequest{
				RepoDir:     dir,
				PullRequest: doc.PullRequest,
				User:        user,
				DryRun:      doc.DryRun,
			})
			doc.Released = append(doc.Released, released...)
			return err
		})
	}
	if err := checkCluster(doc.Cluster); err != nil {
		return err
	}
	if err := validate.Name("app", app); err != nil {
		return usage("%v", err)
	}
	what := fmt.Sprintf("the release of %s on %s", app, doc.Cluster)
	return s.inCheckout(ctx, request, &doc.reservationHead, what, func(dir, user string) error {
		var result reservation.ReleaseResult
		err := reservation.PushWithRetry(ctx, dir, doc.DryRun, func() error {
			var err error
			result, err = reservation.Release(reservation.ReleaseRequest{
				RepoDir:     dir,
				Cluster:     doc.Cluster,
				App:         app,
				User:        user,
				PullRequest: doc.PullRequest,
			})
			return err
		})
		if err == nil {
			doc.ReleaseResult = &result
		}
		return err
	})
}
