// Package authexec runs another program with the App login's GitHub token in
// its environment: `devctl auth exec`, and devctl started under a proxied
// name (a `gh` link to devctl), which runs the next `gh` on PATH so. The
// token reaches the program's environment only; devctl never prints it.
package authexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// EnvToken is the variable the program finds the token in: gh's own, which
// it prefers over its stored login.
const EnvToken = "GH_TOKEN"

// MinValidity is how long the token stays valid at least when the program
// starts: a stored token that expires sooner is refreshed first, so a
// command started on it does not see it expire.
const MinValidity = 10 * time.Minute

// EnvChain is the variable that carries the programs devctl ran so far in
// one chain of calls, separated like PATH: devctl started again under it,
// by the program or something it ran, adds the program it runs next.
const EnvChain = "DEVCTL_AUTH_EXEC_CHAIN"

// MaxChain is how many programs one chain holds before devctl refuses to run
// another: a program that hands the call back to devctl (a gh shim passing
// it to the first gh on PATH, the link) loops, and a program legitimately
// calling gh again (a gh alias) nests far less deep.
const MaxChain = 4

// proxies are the names devctl answers to as a proxy: started as one of them
// (a link named so), it runs the program of that name with the token.
var proxies = map[string]bool{"gh": true}

// Proxy is the program devctl runs for the name it was started as (argv[0]),
// "" when that name is devctl's own.
func Proxy(argv0 string) string {
	name := strings.TrimSuffix(filepath.Base(argv0), ".exe")
	if proxies[name] {
		return name
	}
	return ""
}

// Config is what [Run] needs from its process; [Default] fills it in.
type Config struct {
	// Token returns the GitHub token valid for at least the duration.
	Token func(ctx context.Context, valid time.Duration) (authstore.Token, error)
	// Self is devctl's own executable, skipped on PATH so a proxy never
	// runs itself.
	Self string
	// Path and Environ are the process's PATH and environment.
	Path    string
	Environ []string
	// Remotes is the working directory's git remote configuration, which
	// names the repository a gh invocation without --repo acts on.
	Remotes func() string
	Stderr  io.Writer
	// Exec replaces the process with the program (or runs it and returns
	// its exit code where the platform cannot replace a process).
	Exec func(path string, argv, env []string) (int, error)
}

// Default is the Config of this process.
func Default() Config {
	self, _ := os.Executable()
	return Config{
		Token:   authstore.RequireGitHubFor,
		Self:    self,
		Path:    os.Getenv("PATH"),
		Environ: os.Environ(),
		Remotes: gitRemotes,
		Stderr:  os.Stderr,
		Exec:    execProgram,
	}
}

// Run runs name with args and the token in [EnvToken] and returns the exit
// code to leave with: the program's, or 8 without a usable login and 7 when
// the program is not found or the chain of calls loops ([MaxChain]), each
// with one line on stderr naming the cause.
// gh acting on a repository of an owner outside [AppOwners], or uploading a
// file ([GHAttaches]), runs without the token, on the person's own gh login.
func Run(ctx context.Context, c Config, name string, args []string) int {
	path, err := LookPath(name, c.Path, c.Self)
	if err != nil {
		fmt.Fprintf(c.Stderr, "devctl auth exec: %s\n", err)
		return agentcli.ExitUsage
	}
	environ, err := chained(c.Environ, path, c.Self)
	if err != nil {
		fmt.Fprintf(c.Stderr, "devctl auth exec: %s\n", err)
		return agentcli.ExitUsage
	}
	argv := append([]string{name}, args...)
	if strings.TrimSuffix(filepath.Base(name), ".exe") == "gh" {
		if owner := GHOwner(args, environ, c.Remotes); (owner != "" && !AppOwners[owner]) || GHAttaches(args) {
			return c.run(path, argv, withoutGHTokens(environ))
		}
	}
	token, err := c.Token(ctx, MinValidity)
	if err != nil {
		fmt.Fprintf(c.Stderr, "devctl auth exec: %s\n", err)
		var coder agentcli.ExitCoder
		if errors.As(err, &coder) {
			return coder.ExitCode()
		}
		return agentcli.ExitUsage
	}
	return c.run(path, argv, Environ(environ, token.Value))
}

// chained is environ with path added to [EnvChain], or an error naming the
// chain when it holds [MaxChain] programs already.
func chained(environ []string, path, self string) ([]string, error) {
	var chain []string
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, EnvChain+"="); ok && v != "" {
			chain = filepath.SplitList(v)
		}
	}
	if len(chain) >= MaxChain {
		return nil, fmt.Errorf("refusing to run %s: devctl (%s) was started again by each program it ran, %s; "+
			"a program on PATH passes the call back to devctl (a gh shim running the first gh on PATH, the gh link) instead of the real one",
			path, self, strings.Join(chain, " -> "))
	}
	chain = append(chain, path)
	return append(without(environ, EnvChain), EnvChain+"="+strings.Join(chain, string(os.PathListSeparator))), nil
}

func (c Config) run(path string, argv, env []string) int {
	code, err := c.Exec(path, argv, env)
	if err != nil {
		fmt.Fprintf(c.Stderr, "devctl auth exec: running %s: %s\n", path, err)
		return agentcli.ExitUsage
	}
	return code
}

// Environ is environ with [EnvToken] set to token, any earlier value of it
// replaced.
func Environ(environ []string, token string) []string {
	return append(without(environ, EnvToken), EnvToken+"="+token)
}

// without is environ without the variable name.
func without(environ []string, name string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// LookPath is the program name names: a path as given, a bare name the first
// executable of that name on path past the last entry that is self (devctl,
// also through a link), so a `gh` link to devctl runs the real gh and never a
// gh shim ahead of the link, which handed the call to the link and would hand
// it back. Without such an entry, or nothing past it, it is the first
// executable of that name on path that is not self.
func LookPath(name, path, self string) (string, error) {
	selfResolved := resolve(self)
	if strings.ContainsRune(name, filepath.Separator) || strings.ContainsRune(name, '/') {
		if resolve(name) == selfResolved {
			return "", fmt.Errorf("%s is devctl itself", name)
		}
		if !executable(name) {
			return "", fmt.Errorf("%s is not an executable file", name)
		}
		return name, nil
	}
	var found []string
	past := 0
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		for _, candidate := range candidates(filepath.Join(dir, name)) {
			if !executable(candidate) {
				continue
			}
			if resolve(candidate) == selfResolved {
				past = len(found)
				continue
			}
			found = append(found, candidate)
		}
	}
	switch {
	case past < len(found):
		return found[past], nil
	case len(found) > 0:
		return found[0], nil
	}
	return "", fmt.Errorf("%s: not found on PATH (devctl itself skipped)", name)
}

// resolve is path with every link resolved, path itself when that fails.
func resolve(path string) string {
	if path == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && isExecutable(info.Mode())
}
