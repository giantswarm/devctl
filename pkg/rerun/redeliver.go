package rerun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// StalledAfter is how long a pipeline may go without a workflow before its
// push webhook is redelivered: a setup workflow continues within a minute or
// two, and a continuation CircleCI accepted is created within seconds.
const StalledAfter = 5 * time.Minute

// The outcomes of a redelivery.
const (
	// OutcomeRedelivered: the push delivery was sent again; CircleCI creates a
	// new pipeline for it.
	OutcomeRedelivered = "redelivered"
	// OutcomeAlreadyRedelivered: the push delivery was sent again before and
	// the pipeline it created has no workflow either; a second redelivery is
	// not sent.
	OutcomeAlreadyRedelivered = "already_redelivered"
)

// Hooks is the GitHub webhook API of a repository: the part of
// [githubclient.Client] a redelivery uses.
type Hooks interface {
	ListHooks(ctx context.Context, owner, repo string) ([]*github.Hook, error)
	ListHookDeliveries(ctx context.Context, owner, repo string, hookID int64) ([]*github.HookDelivery, error)
	HookDelivery(ctx context.Context, owner, repo string, hookID, deliveryID int64) (*github.HookDelivery, error)
	RedeliverHookDelivery(ctx context.Context, owner, repo string, hookID, deliveryID int64) error
}

// Redelivery is the way out of a pipeline that never got a workflow (its
// setup workflow finished and the continuation was never created, or the
// pipeline stays pending without a workflow): the push webhook delivery that
// created it is sent again, so CircleCI creates a new pipeline for the head.
// Once per head: a delivery sent again before is not sent a second time.
type Redelivery struct {
	// Hooks is asked for the webhook API and the identity it acts as
	// ([authexec.Identity]) only when the pipeline has no workflow; nil
	// reports such a pipeline without a redelivery (exit 5).
	Hooks func(ctx context.Context) (Hooks, string, error)
	// Ref is the git ref the push carried, refs/tags/<tag> for a tag, which
	// identifies the delivery; empty for a branch head, whose delivery is the
	// push of the pipeline's revision.
	Ref string
	// Now is the clock; nil reads time.Now.
	Now func() time.Time
}

// RedeliveryResult is the redelivery in the command's document: the hook,
// the delivery sent again and what came of it.
type RedeliveryResult struct {
	HookID      int64     `json:"hookId"`
	HookURL     string    `json:"hookUrl"`
	DeliveryID  int64     `json:"deliveryId"`
	GUID        string    `json:"guid"`
	DeliveredAt time.Time `json:"deliveredAt"`
	// Ref and After are the push as the delivery carried it.
	Ref   string `json:"ref"`
	After string `json:"after"`
	// Outcome is one of redelivered, already_redelivered, refused.
	Outcome string `json:"outcome"`
	// RedeliveredAt is when the delivery was sent again before
	// (already_redelivered).
	RedeliveredAt *time.Time `json:"redeliveredAt,omitempty"`
}

// stalled says whether a pipeline never got a workflow to rerun: it is still
// continuing (its setup workflow done, the continuation not created) or has
// no workflow at all, and nothing of it failed or still runs. A failed setup
// workflow is rerun from failed like any other; a running one is waited for.
func stalled(pipeline *circleciclient.Pipeline, newest []circleciclient.Workflow) bool {
	if !circleciclient.PipelineContinuing(pipeline.State) && len(newest) > 0 {
		return false
	}
	for _, run := range newest {
		if !circleciclient.WorkflowSucceeded(run.Status) {
			return false
		}
	}
	return true
}

func (r Redelivery) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// redeliver sends the push delivery of pipeline again, or says why not:
// exit 5 for a pipeline younger than [StalledAfter] (CircleCI may still
// create the workflow) and for a delivery sent again before, exit 3 for a
// repository without a CircleCI webhook or without the head's push delivery,
// exit 8 for a 403, naming the permission the identity lacks.
func (r Redelivery) redeliver(ctx context.Context, org, repo string, pipeline *circleciclient.Pipeline, result *Result, warn func(string)) error {
	age := r.now().Sub(pipeline.CreatedAt).Round(time.Second)
	state := fmt.Sprintf("pipeline %d has no workflow %s after its creation (state %s)", pipeline.Number, age, pipeline.State)
	if age < StalledAfter {
		return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused,
			"%s: CircleCI may still create it; rerun once the pipeline is %s old, then its push webhook is redelivered", state, StalledAfter)
	}
	if r.Hooks == nil {
		return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused,
			"%s: nothing to rerun; redeliver its push webhook from the repository's webhook settings", state)
	}
	hooks, identity, err := r.Hooks(ctx)
	if err != nil {
		return err
	}
	all, err := hooks.ListHooks(ctx, org, repo)
	if err != nil {
		return forbidden(err, identity, org, repo, "the webhook listing")
	}
	hook := circleCIHook(all)
	if hook == nil {
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
			"%s, and %s/%s has no CircleCI webhook among its %d (none posts to circleci.com): nothing to redeliver", state, org, repo, len(all))
	}
	deliveries, err := hooks.ListHookDeliveries(ctx, org, repo, hook.GetID())
	if err != nil {
		return forbidden(err, identity, org, repo, "the delivery listing")
	}
	// A redelivery is a newer entry with the delivery's guid; newest first,
	// so every redelivery of a push is seen before the push itself.
	redelivered := map[string]time.Time{}
	var match *github.HookDelivery
	var push pushPayload
	for _, d := range deliveries {
		if d.GetEvent() != "push" {
			continue
		}
		if d.GetRedelivery() {
			if _, seen := redelivered[d.GetGUID()]; !seen {
				redelivered[d.GetGUID()] = d.GetDeliveredAt().Time
			}
			continue
		}
		full, err := hooks.HookDelivery(ctx, org, repo, hook.GetID(), d.GetID())
		if err != nil {
			return forbidden(err, identity, org, repo, "the delivery")
		}
		if p, ok := r.matches(full, pipeline.VCS.Revision); ok {
			match, push = full, p
			break
		}
	}
	if match == nil {
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
			"%s, and none of the newest %d deliveries of its CircleCI webhook is the push of %s: nothing to redeliver (a head in a fork is pushed to the fork)",
			state, len(deliveries), r.what(pipeline))
	}
	result.Redelivery = &RedeliveryResult{
		HookID: hook.GetID(), HookURL: hook.GetConfig().GetURL(),
		DeliveryID: match.GetID(), GUID: match.GetGUID(), DeliveredAt: match.GetDeliveredAt().Time,
		Ref: push.Ref, After: push.After,
	}
	if at, ok := redelivered[match.GetGUID()]; ok {
		result.Redelivery.Outcome, result.Redelivery.RedeliveredAt = OutcomeAlreadyRedelivered, &at
		return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused,
			"%s, and its push delivery %s was already sent again at %s: a second redelivery is not sent; "+
				"check CircleCI's status, or push an empty commit", state, match.GetGUID(), at.UTC().Format(time.RFC3339))
	}
	if err := hooks.RedeliverHookDelivery(ctx, org, repo, hook.GetID(), match.GetID()); err != nil {
		result.Redelivery.Outcome = OutcomeRefused
		return forbidden(err, identity, org, repo, "the redelivery")
	}
	result.Redelivery.Outcome = OutcomeRedelivered
	warn(fmt.Sprintf("%s: its push delivery %s (%s at %s, delivered %s) was sent again; CircleCI creates a new pipeline for the head, the one the wait reads",
		state, match.GetGUID(), push.Ref, push.After, match.GetDeliveredAt().UTC().Format(time.RFC3339)))
	return nil
}

// what names the push the redelivery looks for.
func (r Redelivery) what(pipeline *circleciclient.Pipeline) string {
	if r.Ref != "" {
		return r.Ref
	}
	return pipeline.VCS.Revision
}

// pushPayload is what identifies a push delivery: the ref it moved, the
// revision it moved it to, and whether it deleted the ref.
type pushPayload struct {
	Ref     string `json:"ref"`
	After   string `json:"after"`
	Deleted bool   `json:"deleted"`
}

// matches says whether delivery is the push that created the pipeline: of
// the ref when one is named, else of revision. A deletion moves nothing.
func (r Redelivery) matches(delivery *github.HookDelivery, revision string) (pushPayload, bool) {
	var p pushPayload
	if delivery.Request == nil || delivery.Request.RawPayload == nil || json.Unmarshal(*delivery.Request.RawPayload, &p) != nil || p.Deleted {
		return p, false
	}
	if r.Ref != "" {
		return p, p.Ref == r.Ref
	}
	return p, p.After == revision
}

// circleCIHook is the first active webhook posting to circleci.com, nil
// without one.
func circleCIHook(hooks []*github.Hook) *github.Hook {
	for _, hook := range hooks {
		u, err := url.Parse(hook.GetConfig().GetURL())
		if err != nil || !hook.GetActive() {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host == "circleci.com" || strings.HasSuffix(host, ".circleci.com") {
			return hook
		}
	}
	return nil
}

// forbidden turns GitHub's 403 on the webhook API into exit 8 naming the
// permission the identity lacks: the devctl App's repository permission
// Webhooks (read and write), which an owner of the App grants, or the
// person's own access to the repository's webhooks. Any other error is
// returned unchanged.
func forbidden(err error, identity, org, repo, what string) error {
	if !githubclient.IsForbidden(err) {
		return err
	}
	if identity == authexec.IdentityGH {
		return agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired,
			"GitHub refused %s of %s/%s (403): your gh login cannot manage its webhooks; it takes a repository admin's login with the repo or admin:repo_hook scope, "+
				"or a repository admin redelivers the push delivery under the repository's Settings → Webhooks", what, org, repo)
	}
	return agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired,
		"GitHub refused %s of %s/%s (403): the giantswarm-devctl App lacks the repository permission Webhooks: read and write; "+
			"an owner of the App grants it (the App's settings → Permissions & events → Repository permissions → Webhooks → Read and write) and the organization "+
			"approves the updated permissions on its installation; until then a repository admin redelivers the push delivery under the repository's Settings → Webhooks",
		what, org, repo)
}

// NewGitHub is the GitHub client of owner's repositories on the identity
// [authexec.RepositoryToken] picks (the App login for an owner the App is
// installed on, the person's gh login elsewhere) and that token, built the
// way devctl pr wait builds its own; reads go through transport.
func NewGitHub(ctx context.Context, owner string, app, person func(context.Context) (authstore.Token, error), endpoints agentcli.Endpoints, transport http.RoundTripper) (*githubclient.Client, authstore.Token, error) {
	token, err := authexec.RepositoryToken(ctx, owner, app, person)
	if err != nil {
		return nil, authstore.Token{}, err
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	client, _, err := githubclient.NewConditional(githubclient.Config{
		Logger:      logger,
		AccessToken: token.Value,
		BaseURL:     endpoints.GitHubAPIURL,
		Transport:   transport,
	})
	if err != nil {
		return nil, authstore.Token{}, err
	}
	return client, token, nil
}
