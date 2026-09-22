package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

const command = "auth login"

// The sign-in to giantswarm-repo-manager after the muster login: how long the
// human has to authorize the GitHub App, and how often the manager is asked
// whether they did.
const (
	managerSignInTimeout  = 10 * time.Minute
	managerSignInInterval = 3 * time.Second
)

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// open is authstore.Open; tests inject an Auth over fakes.
	open func(stderr io.Writer) (*authstore.Auth, error)
	// clock paces the manager sign-in.
	clock agentcli.Clock
	// openBrowser opens the manager's consent URL; nil means the platform's
	// opener.
	openBrowser func(url string) error
}

// document is the command's JSON: the envelope, the identities and, after a
// muster login, the sign-in to giantswarm-repo-manager.
type document struct {
	agentcli.Envelope
	authstore.Status
	Manager *managerSignIn `json:"giantswarmRepoManager,omitempty"`
}

// managerSignIn is what became of the sign-in to giantswarm-repo-manager
// through muster: the person's GitHub App consent, given once.
type managerSignIn struct {
	Server string `json:"server"`
	// SignedIn says the manager answers the person's calls now.
	SignedIn bool `json:"signedIn"`
	// Caller is the GitHub login the manager sees, once signed in.
	Caller string `json:"caller,omitempty"`
	// Note says why the sign-in did not happen, when it did not.
	Note string `json:"note,omitempty"`
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	return r.run(context.Background())
}

func (r *runner) run(ctx context.Context) error {
	doc := document{Envelope: agentcli.NewEnvelope(command, time.Now()), Status: authstore.NewStatus()}
	err := r.login(ctx, &doc)
	doc.Finish(time.Now(), agentcli.VerdictGreen, err)
	if err := agentcli.Emit(r.stdout, doc); err != nil {
		return err
	}
	return doc.Err()
}

func (r *runner) login(ctx context.Context, doc *document) error {
	if err := r.flag.Validate(); err != nil {
		return err
	}
	auth, err := r.open(r.stderr)
	if err != nil {
		return err
	}
	progress := agentcli.NewProgress(r.stderr, r.flag.Progress)

	switch {
	case r.flag.MusterOnly:
		progress.Printf("muster: signing in to %s", r.flag.MusterEndpoint)
		id, err := auth.LoginMuster(ctx, r.flag.MusterEndpoint)
		if err != nil {
			return err
		}
		progress.Printf("muster: signed in as %s, token stored in the keychain", id.Login)
		token, err := auth.RequireMuster(ctx)
		if err != nil {
			return err
		}
		doc.Manager, err = r.signInManager(ctx, token, progress)
		if err != nil {
			return err
		}
	default:
		if !r.flag.CircleCIOnly {
			progress.Printf("GitHub: requesting a device code")
			id, err := auth.LoginGitHub(ctx)
			if err != nil {
				return err
			}
			progress.Printf("GitHub: logged in as %s, token stored in the keychain", id.Login)
		}
		if !r.flag.GitHubOnly {
			progress.Printf("CircleCI: starting the authorization")
			id, err := auth.LoginCircleCI(ctx)
			if err != nil {
				return err
			}
			progress.Printf("CircleCI: logged in as %s, token stored in the keychain", id.Login)
		}
	}

	status, err := auth.Status()
	if err != nil {
		return err
	}
	doc.Status = status
	for _, w := range status.CircleCI.Warnings {
		doc.Warn(w)
	}
	if doc.Manager != nil && doc.Manager.Note != "" {
		doc.Warn(doc.Manager.Note)
	}
	return nil
}

// urlPattern finds the authorization URL in muster's sign-in answer.
var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// signInManager asks muster to sign the person in to giantswarm-repo-manager:
// the manager is pinned to its GitHub App, so muster answers either that the
// person is signed in already or the URL of the App's consent, which devctl
// prints and opens; then it asks the manager who is calling until it
// answers, which is when the consent is given. A muster without the manager
// is a note, not a failure: the login itself stands.
func (r *runner) signInManager(ctx context.Context, token authstore.Token, progress *agentcli.Progress) (*managerSignIn, error) {
	client := &manager.Client{Endpoint: token.Endpoint, Token: token.Value, Version: project.Version()}
	signIn := &managerSignIn{Server: manager.Server}

	progress.Printf("%s: asking muster for the sign-in", manager.Server)
	answer, err := client.Call(ctx, manager.ToolAuthLogin, map[string]any{"server": manager.Server})
	switch {
	case err == nil:
	case manager.IsTool(err) && strings.Contains(err.Error(), "not found"):
		signIn.Note = fmt.Sprintf("%s is not registered at %s: the repo commands need a muster that runs it", manager.Server, token.Endpoint)
		return signIn, nil
	default:
		return nil, err
	}

	text := strings.Trim(string(answer), "\"")
	if url := urlPattern.FindString(text); url != "" {
		fmt.Fprintf(r.stderr, "%s: open %s and authorize the GitHub App %s\n", manager.Server, url, manager.Server)
		if err := r.browse(url); err != nil {
			fmt.Fprintf(r.stderr, "(no browser opened: %v; open the URL yourself)\n", err)
		}
	} else {
		progress.Printf("%s: %s", manager.Server, strings.TrimSpace(text))
	}

	caller, err := r.awaitManager(ctx, client, progress)
	if err != nil {
		return nil, err
	}
	signIn.SignedIn, signIn.Caller = true, caller
	progress.Printf("%s: signed in as %s", manager.Server, caller)
	return signIn, nil
}

func (r *runner) browse(url string) error {
	if r.openBrowser != nil {
		return r.openBrowser(url)
	}
	return authstore.OpenBrowser(url)
}

// awaitManager calls the manager's get_info until it answers, which it does
// once the person's consent is filed under them in muster; the manager's
// refusals meanwhile are the wait, an endpoint that stops answering ends it.
func (r *runner) awaitManager(ctx context.Context, client *manager.Client, progress *agentcli.Progress) (string, error) {
	waitCtx, cancel := r.clock.Timeout(ctx, managerSignInTimeout)
	defer cancel()
	var last error
	for {
		answer, err := client.Call(waitCtx, manager.ToolGetInfo, nil)
		if err == nil {
			return callerOf(answer), nil
		}
		if manager.IsUnreachable(err) || manager.IsAuthRequired(err) {
			return "", err
		}
		last = err
		progress.Printf("%s: not signed in yet (%v)", manager.Server, err)
		if err := r.clock.Sleep(waitCtx, managerSignInInterval); err != nil {
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("the sign-in to %s was not completed in time; the last answer: %v", manager.Server, last)
			}
			return "", err
		}
	}
}

// callerOf reads the caller's GitHub login from get_info's answer.
func callerOf(info []byte) string {
	var doc struct {
		Caller struct {
			Login string `json:"login"`
		} `json:"caller"`
	}
	if err := json.Unmarshal(info, &doc); err != nil {
		return ""
	}
	return doc.Caller.Login
}
