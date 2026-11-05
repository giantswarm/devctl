package followcircleci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
)

// OptOutEnv turns the automatic pass off when set to anything.
const OptOutEnv = "DEVCTL_NO_CIRCLECI_PENDING"

// The automatic pass runs at most once every passInterval, says what to do
// about a login at most once every noticeInterval, and gives up after
// passTimeout.
const (
	passInterval   = 6 * time.Hour
	noticeInterval = 24 * time.Hour
	passTimeout    = 20 * time.Second
)

// stateFile is the pass's state in devctl's configuration directory.
const stateFile = "circleci-pending.json"

// skipFirst are the commands, by their first word below devctl, the pass
// never follows: a long-running server, shell completion, the version.
var skipFirst = map[string]bool{
	"serve":            true,
	"completion":       true,
	"version":          true,
	"help":             true,
	"__complete":       true,
	"__completeNoDesc": true,
}

// skipPath are the commands the pass never follows by their full path below
// devctl: a logout, a proxied program, the explicit run.
var skipPath = map[string]bool{
	"auth logout":          true,
	"auth exec":            true,
	"repo follow-circleci": true,
}

// Auto is the automatic pass after a devctl command: at most once every six
// hours, with the logins the keychain holds and never a prompt, it sets up
// the pending CircleCI pipelines of the person's teams. It never fails: the
// command's exit code and stdout stay as they are, and stderr carries a line
// per pipeline it created and, at most once a day, what to do about a login.
func Auto(executed *cobra.Command, stderr io.Writer) {
	a := &autoPass{
		getenv: os.Getenv,
		state:  filepath.Join(env.ConfigDir.Val(), stateFile),
		now:    time.Now,
		stderr: stderr,
		pass:   &pendingPass{open: client.Open, openCI: circleciapp.OpenAsPerson},
	}
	a.run(context.Background(), executed)
}

type autoPass struct {
	getenv func(string) string
	state  string
	now    func() time.Time
	stderr io.Writer
	pass   *pendingPass
}

// autoState is what the pass remembers between commands.
type autoState struct {
	LastRun    time.Time `json:"lastRun"`
	LastNotice time.Time `json:"lastNotice"`
}

func (a *autoPass) run(ctx context.Context, executed *cobra.Command) {
	if a.getenv(OptOutEnv) != "" || !follows(executed) {
		return
	}
	state := a.load()
	now := a.now()
	if now.Sub(state.LastRun) < passInterval {
		return
	}
	// Claim the window before the pass: a state that cannot be written
	// would otherwise run the pass after every command.
	state.LastRun = now
	if err := a.save(state); err != nil {
		a.debugf(err)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()
	var warnings []string
	a.pass.warn = func(w string) { warnings = append(warnings, w) }
	summary, err := a.pass.run(ctx)
	if summary == nil {
		// No list: no muster login, or the manager did not answer.
		a.debugf(err)
		return
	}
	for _, e := range summary.Repositories {
		if e.Outcome == OutcomeCreated || (e.Outcome == OutcomeAlreadySetUp && len(e.Runs) > 0) {
			fmt.Fprintf(a.stderr, "devctl: %s: %s\n", e.Repository, describe(e))
		}
	}
	var authErr *authstore.AuthRequiredError
	if !errors.As(err, &authErr) {
		a.debugf(err)
		return
	}
	if now.Sub(state.LastNotice) < noticeInterval {
		return
	}
	for _, w := range warnings {
		fmt.Fprintf(a.stderr, "devctl: %s\n", w)
	}
	msg := authErr.Error()
	if !strings.Contains(msg, "grant Write") {
		msg += " On CircleCI's consent page, grant Write access."
	}
	fmt.Fprintf(a.stderr, "devctl: %s: %s\n", waiting(summary), msg)
	state.LastNotice = now
	if err := a.save(state); err != nil {
		a.debugf(err)
	}
}

// debugf writes err on stderr at --log-level debug; stdout, where logrus
// writes, belongs to the command.
func (a *autoPass) debugf(err error) {
	if err != nil && logrus.IsLevelEnabled(logrus.DebugLevel) {
		fmt.Fprintf(a.stderr, "devctl: debug: circleci pending pass: %v\n", err)
	}
}

// follows says whether the pass runs after executed.
func follows(executed *cobra.Command) bool {
	if executed == nil || !executed.HasParent() {
		return false
	}
	if help := executed.Flags().Lookup("help"); help != nil && help.Changed {
		return false
	}
	path := strings.Fields(executed.CommandPath())[1:]
	return !skipFirst[path[0]] && !skipPath[strings.Join(path, " ")]
}

// waiting names the repositories the pass did not set up.
func waiting(s *Summary) string {
	var repos []string
	for _, e := range s.Repositories {
		if e.Outcome != OutcomeCreated && e.Outcome != OutcomeAlreadySetUp {
			repos = append(repos, e.Repository)
		}
	}
	noun, verb := "repositories", "wait"
	if len(repos) == 1 {
		noun, verb = "repository", "waits"
	}
	return fmt.Sprintf("%d %s of your teams %s for a CircleCI pipeline (%s)", len(repos), noun, verb, strings.Join(repos, ", "))
}

func (a *autoPass) load() autoState {
	var s autoState
	b, err := os.ReadFile(a.state)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return autoState{}
	}
	return s
}

func (a *autoPass) save(s autoState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.state), 0o700); err != nil {
		return err
	}
	tmp := a.state + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.state)
}
