package followcircleci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rogpeppe/go-internal/lockedfile"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/project"
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

// guardGrace is how long the pass gets past its own timeout to stop before
// it is abandoned.
const guardGrace = 2 * time.Second

// Auto is the automatic pass after a devctl command: at most once every six
// hours, with the logins the keychain holds and no Secret Service unlock
// prompt on Linux, it sets up the pending CircleCI pipelines of the person's
// teams. It never fails: the command's exit code and stdout stay as they
// are, and stderr carries a line per pipeline it created and, at most once a
// day, what to do about a login. A panic is swallowed, a locked Linux
// keychain is skipped (a save may still prompt, and the macOS keychain or
// KeePassXC may ask: [authstore.NoPrompt]), and the pass is abandoned after
// passTimeout or on an interrupt.
func Auto(executed *cobra.Command, stderr io.Writer) {
	defer func() { _ = recover() }()
	// Checked here as well as in run: before the state path is resolved and
	// the signal handler set up, for the commands that skip the pass.
	if off(os.Getenv) || !follows(executed) {
		return
	}
	dir, err := configDir()
	if err != nil {
		return
	}
	a := &autoPass{
		getenv:   os.Getenv,
		noPrompt: authstore.NoPrompt,
		state:    filepath.Join(dir, stateFile),
		now:      time.Now,
		stderr:   stderr,
		pass:     &pendingPass{open: client.Open, openCI: circleciapp.OpenAsPerson},
	}
	// An interrupt ends the pass, not the process: the exit code stays the
	// command's.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	guard(ctx, passTimeout, func(ctx context.Context) { a.run(ctx, executed) })
}

// guard runs fn with a context that ends after timeout and returns when fn
// does, timeout plus guardGrace has passed or ctx ends, whichever comes
// first: a keychain call or a lock that ignores the context is abandoned, and
// the process exit ends it. A panic in fn is swallowed.
func guard(ctx context.Context, timeout time.Duration, fn func(context.Context)) {
	passCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		fn(passCtx)
	}()
	t := time.NewTimer(timeout + guardGrace)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	case <-ctx.Done():
	}
}

// configDir is devctl's configuration directory, as [env.ConfigDir] names it,
// with an error where that panics: no XDG_CONFIG_HOME and no home.
func configDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, project.Name()), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", project.Name()), nil
}

type autoPass struct {
	getenv func(string) string
	// noPrompt refuses keychain prompts for the rest of the process, once
	// the pass claimed its window.
	noPrompt func()
	state    string
	now      func() time.Time
	stderr   io.Writer
	pass     *pendingPass
}

// autoState is what the pass remembers between commands.
type autoState struct {
	LastRun    time.Time `json:"lastRun"`
	LastNotice time.Time `json:"lastNotice"`
}

func (a *autoPass) run(ctx context.Context, executed *cobra.Command) {
	if off(a.getenv) || !follows(executed) {
		return
	}
	now := a.now()
	if !a.claimRun(now) {
		return
	}
	// The process ends after the pass: no Secret Service unlock or delete
	// prompt for it.
	if a.noPrompt != nil {
		a.noPrompt()
	}

	var warnings []string
	a.pass.warn = func(w string) { warnings = append(warnings, w) }
	summary, err := a.pass.run(ctx)
	if summary == nil {
		// No list: no muster login, or the manager did not answer.
		a.debug(err)
		return
	}
	for _, e := range summary.Repositories {
		if e.Outcome == OutcomeCreated || (e.Outcome == OutcomeAlreadySetUp && len(e.Runs) > 0) {
			fmt.Fprintf(a.stderr, "devctl: %s: %s\n", e.Repository, describe(e))
		}
	}
	msg := notice(err)
	if msg == "" {
		a.debug(err)
		return
	}
	if !a.claimNotice(now) {
		return
	}
	for _, w := range warnings {
		fmt.Fprintf(a.stderr, "devctl: %s\n", w)
	}
	fmt.Fprintf(a.stderr, "devctl: %s: %s\n", waiting(summary), msg)
}

// notice is what the person can do about err, or empty when only the next
// pass can: a login to renew, or a repository admin to ask.
func notice(err error) string {
	var authErr *authstore.AuthRequiredError
	if errors.As(err, &authErr) {
		msg := authErr.Error()
		if authErr.Identity == authstore.IdentityCircleCI && !strings.Contains(msg, "grant Write") {
			msg += " On CircleCI's consent page, grant Write access."
		}
		return msg
	}
	var exitErr *agentcli.ExitError
	if errors.As(err, &exitErr) && exitErr.Code == agentcli.ExitRefused {
		return exitErr.Reason
	}
	return ""
}

// debug writes err on stderr at --log-level debug; stdout, where logrus
// writes, belongs to the command.
func (a *autoPass) debug(err error) {
	if err != nil && logrus.IsLevelEnabled(logrus.DebugLevel) {
		fmt.Fprintf(a.stderr, "devctl: debug: circleci pending pass: %v\n", err)
	}
}

// off says whether the environment turns the pass off: the opt-out, or a CI
// system, where devctl never reads the keychain.
func off(getenv func(string) string) bool {
	return getenv(OptOutEnv) != "" || getenv(authstore.EnvCI) != ""
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

// claimRun takes the six-hour window for this process: under the state's
// lock, so of two commands finishing together only one runs the pass. A
// state that cannot be written claims nothing; the pass would otherwise run
// after every command.
func (a *autoPass) claimRun(now time.Time) bool {
	return a.update(func(s *autoState) bool {
		if now.Sub(s.LastRun) < passInterval && !s.LastRun.After(now) {
			return false
		}
		s.LastRun = now
		return true
	})
}

// claimNotice takes the day's notice, as claimRun the window.
func (a *autoPass) claimNotice(now time.Time) bool {
	return a.update(func(s *autoState) bool {
		if now.Sub(s.LastNotice) < noticeInterval && !s.LastNotice.After(now) {
			return false
		}
		s.LastNotice = now
		return true
	})
}

// update reads the state, applies change and writes it when change says so,
// under the state's lock.
func (a *autoPass) update(change func(*autoState) bool) bool {
	if err := os.MkdirAll(filepath.Dir(a.state), 0o700); err != nil {
		a.debug(err)
		return false
	}
	unlock, err := lockedfile.MutexAt(a.state + ".lock").Lock()
	if err != nil {
		a.debug(err)
		return false
	}
	defer unlock()
	s := a.load()
	if !change(&s) {
		return false
	}
	if err := a.save(s); err != nil {
		a.debug(err)
		return false
	}
	return true
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
	tmp, err := os.CreateTemp(filepath.Dir(a.state), stateFile+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), a.state)
}
