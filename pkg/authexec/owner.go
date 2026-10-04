package authexec

import (
	"net/url"
	"os/exec"
	"strings"
)

// AppOwners are the accounts the giantswarm-devctl App is installed on: gh
// acts with the App token for their repositories and wherever no repository
// is named. A repository of another owner is out of the App's reach, so gh
// keeps the person's own login for it.
var AppOwners = map[string]bool{"giantswarm": true}

// repoCommands are gh's commands whose first argument may be a repository
// or a URL of one (`gh pr view <url>`, `gh repo view <owner/repo>`).
var repoCommands = map[string]bool{
	"issue": true, "pr": true, "release": true, "repo": true, "run": true, "workflow": true,
}

// GHOwner is the owner of the repository a gh invocation acts on, "" when it
// names none: --repo/-R, $GH_REPO, a repository path of `gh api`, the URL or
// repository argument of a repository command, else the repository of the
// working directory, resolved like gh (its default, then the remotes
// upstream, github and origin). remotes reads the working directory's
// remote configuration; nil reads none.
func GHOwner(args []string, environ []string, remotes func() string) string {
flags:
	for i, a := range args {
		switch {
		case a == "--":
			break flags
		case (a == "-R" || a == "--repo") && i+1 < len(args):
			return repoOwner(args[i+1])
		case strings.HasPrefix(a, "--repo="):
			return repoOwner(strings.TrimPrefix(a, "--repo="))
		case strings.HasPrefix(a, "-R") && len(a) > 2:
			return repoOwner(a[2:])
		}
	}
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, "GH_REPO="); ok && v != "" {
			return repoOwner(v)
		}
	}
	if len(args) >= 2 && args[0] == "api" {
		if owner, named := apiOwner(args[1:]); named {
			if owner != "{owner}" {
				return owner
			}
			return remoteOwner(remotes)
		}
		return ""
	}
	if len(args) >= 3 && repoCommands[args[0]] && !strings.HasPrefix(args[2], "-") {
		if owner := urlOwner(args[2]); owner != "" {
			return owner
		}
		if args[0] == "repo" && strings.Count(args[2], "/") == 1 {
			return repoOwner(args[2])
		}
	}
	return remoteOwner(remotes)
}

// apiValueFlags are the flags of `gh api` that take a separate value.
var apiValueFlags = map[string]bool{
	"-X": true, "--method": true, "-H": true, "--header": true, "-f": true, "--raw-field": true,
	"-F": true, "--field": true, "-q": true, "--jq": true, "-t": true, "--template": true,
	"--input": true, "--hostname": true, "-p": true, "--preview": true, "--cache": true,
}

// apiOwner is the owner in a `gh api` endpoint (repos/<owner>/…,
// orgs/<owner>/…) and whether the endpoint names one at all.
func apiOwner(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if apiValueFlags[a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(a, "/"), "/")
		if len(parts) >= 2 && (parts[0] == "repos" || parts[0] == "orgs") {
			return strings.ToLower(parts[1]), true
		}
		return "", false
	}
	return "", false
}

// repoOwner is the owner of OWNER/REPO, HOST/OWNER/REPO or a URL.
func repoOwner(repo string) string {
	if owner := urlOwner(repo); owner != "" {
		return owner
	}
	parts := strings.Split(repo, "/")
	if len(parts) < 2 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-2])
}

// urlOwner is the owner of a github.com URL (https, ssh or scp-like git).
func urlOwner(raw string) string {
	if rest, ok := strings.CutPrefix(raw, "git@github.com:"); ok {
		return strings.ToLower(strings.SplitN(rest, "/", 2)[0])
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !strings.EqualFold(strings.TrimPrefix(u.Hostname(), "www."), "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if parts[0] == "" {
		return ""
	}
	return strings.ToLower(parts[0])
}

// remoteOwner is the owner of the working directory's repository as gh
// resolves it: the remote `gh repo set-default` marked, else upstream,
// github, origin, else the first.
func remoteOwner(remotes func() string) string {
	if remotes == nil {
		return ""
	}
	urls := map[string]string{}
	var order []string
	resolved := ""
	for _, line := range strings.Split(remotes(), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		name, field, ok := strings.Cut(strings.TrimPrefix(key, "remote."), ".")
		if !ok {
			continue
		}
		switch field {
		case "url":
			if _, seen := urls[name]; !seen {
				order = append(order, name)
			}
			urls[name] = value
		case "gh-resolved":
			resolved = name
		}
	}
	for _, name := range append([]string{resolved, "upstream", "github", "origin"}, order...) {
		if u, ok := urls[name]; ok {
			return urlOwner(strings.TrimSuffix(u, ".git"))
		}
	}
	return ""
}

// gitRemotes is the remote configuration of the working directory's
// repository, "" outside one.
func gitRemotes() string {
	out, err := exec.Command("git", "config", "--get-regexp", `^remote\.`).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
