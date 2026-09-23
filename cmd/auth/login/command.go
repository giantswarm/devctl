package login

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "login"
	description = "Log in to GitHub (device flow) and CircleCI (OAuth with PKCE); the tokens go into the OS keychain."
	long        = `Log in to GitHub and CircleCI for the agent-facing commands.

GitHub: the device flow of the devctl GitHub App. The verification URL and the
code are printed to stderr and the browser opens the URL; devctl waits until you
have entered the code. The user access token acts as you, capped by the App's
permissions, expires after eight hours and is refreshed by the commands
themselves for six months.

CircleCI: the OAuth 2.0 authorization code flow with PKCE. On the first login
devctl registers itself on this device as a client without a secret (once; the
client id stays in the keychain); every login opens the authorization page,
where Read access is enough. The token is a 90-day CircleCI API token with no
refresh: log in again before it expires; the commands warn seven days ahead.
Re-authorizing revokes the previous token of this device.

Both tokens live in the OS keychain (Secret Service, Keychain, Credential
Manager) with their expiry and are never printed. The command prints one JSON
document, the same as ` + "`devctl auth status`" + `, and exits 0 when the requested
flows completed.`
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	clock, err := agentcli.SystemClock()
	if err != nil {
		return nil, err
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		stderr: config.Stderr,
		stdout: config.Stdout,
		open:   authstore.Open,
		clock:  clock,
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  long,
		// The arguments are checked by the runner: a usage error is exit 7
		// with a document, like every other outcome.
		Args: cobra.ArbitraryArgs,
		RunE: r.Run,
	}
	c.SetFlagErrorFunc(r.FlagError)

	f.Init(c)

	return c, nil
}
