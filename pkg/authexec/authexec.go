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
// the program is not found, each with one line on stderr naming the cause.
// gh acting on a repository of an owner outside [AppOwners] runs without the
// token, on the person's own gh login.
func Run(ctx context.Context, c Config, name string, args []string) int {
	path, err := LookPath(name, c.Path, c.Self)
	if err != nil {
		fmt.Fprintf(c.Stderr, "devctl auth exec: %s\n", err)
		return agentcli.ExitUsage
	}
	argv := append([]string{name}, args...)
	if strings.TrimSuffix(filepath.Base(name), ".exe") == "gh" {
		if owner := GHOwner(args, c.Environ, c.Remotes); owner != "" && !AppOwners[owner] {
			return c.run(path, argv, without(c.Environ, EnvToken))
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
	return c.run(path, argv, Environ(c.Environ, token.Value))
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
// executable of that name on path that is not self (devctl, also through a
// link), so a `gh` link to devctl first on PATH runs the real gh.
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
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		for _, candidate := range candidates(filepath.Join(dir, name)) {
			if executable(candidate) && resolve(candidate) != selfResolved {
				return candidate, nil
			}
		}
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
