package list

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

// The scopes and the filter enums, the manager's.
var (
	scopes     = []string{"mine", "team", "unassigned", "all"}
	renovates  = []string{"configured", "missing", "active", "inactive"}
	visibility = []string{"public", "private"}
	chinaPush  = []string{"split", "inline", "custom", "none"}
	signing    = []string{"signed", "unsigned", "unknown", "none"}
	boolWords  = []string{"true", "false"}
)

type flag struct {
	client.Flags

	Scope        string
	Team         string
	Search       string
	Renovate     string
	Visibility   string
	Fork         string
	Archived     string
	Undeclared   string
	Lifecycle    string
	InactiveDays int
	Finding      string
	Orb          string
	ARM64        string
	ChinaPush    string
	Signing      string
	Limit        int
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd)
	cmd.Flags().StringVar(&f.Scope, "scope", "mine", fmt.Sprintf("Which repositories: %s (mine: the teams you belong to on GitHub; team: --team, or your teams; unassigned: on GitHub without a declaration).", oneOf(scopes)))
	cmd.Flags().StringVar(&f.Team, "team", "", "Only repositories declared by this team (slug: team-bumblebee); under all, none selects the undeclared.")
	cmd.Flags().StringVar(&f.Search, "search", "", "Only repositories whose name or description contains this text.")
	cmd.Flags().StringVar(&f.Renovate, "renovate", "", fmt.Sprintf("Renovate state: %s.", oneOf(renovates)))
	cmd.Flags().StringVar(&f.Visibility, "visibility", "", fmt.Sprintf("Only %s repositories.", oneOf(visibility)))
	cmd.Flags().StringVar(&f.Fork, "fork", "", "Only forks (true) or only non-forks (false).")
	cmd.Flags().StringVar(&f.Archived, "archived", "", "Only repositories whose life is over -- declared archived or deleted, or archived on GitHub (true) -- or only those that are not (false).")
	cmd.Flags().StringVar(&f.Undeclared, "undeclared", "", "Only repositories on GitHub without a declaration (true); the same as --scope unassigned.")
	cmd.Flags().StringVar(&f.Lifecycle, "lifecycle", "", "Only this lifecycle: active, deprecated, archived, deleted.")
	cmd.Flags().IntVar(&f.InactiveDays, "inactive-days", 0, "Only repositories whose last commit by a person is older than this many days, or that have none.")
	cmd.Flags().StringVar(&f.Finding, "finding", "", "Only repositories with a finding of this kind (declared-but-gone, undeclared-on-github, entry-refused, default-icon, ...).")
	cmd.Flags().StringVar(&f.Orb, "orb", "", "Only repositories whose CircleCI pipeline pins this architect orb version, or one starting with it (10 selects every 10.x.y).")
	cmd.Flags().StringVar(&f.ARM64, "arm64", "", "Only repositories whose pipeline builds linux/arm64 images (true) or builds images without it (false).")
	cmd.Flags().StringVar(&f.ChinaPush, "china-push", "", fmt.Sprintf("How the images reach the China registry: %s.", oneOf(chinaPush)))
	cmd.Flags().StringVar(&f.Signing, "signing", "", fmt.Sprintf("Whether images and charts are signed with cosign: %s.", oneOf(signing)))
	cmd.Flags().IntVar(&f.Limit, "limit", 100, "Rows to return.")
}

func (f *flag) Validate() error {
	if err := f.Flags.Validate(); err != nil {
		return microerror.Mask(err)
	}
	for _, check := range []struct {
		name, value string
		allowed     []string
	}{
		{"scope", f.Scope, scopes},
		{"renovate", f.Renovate, renovates},
		{"visibility", f.Visibility, visibility},
		{"china-push", f.ChinaPush, chinaPush},
		{"signing", f.Signing, signing},
		{"fork", f.Fork, boolWords},
		{"archived", f.Archived, boolWords},
		{"undeclared", f.Undeclared, boolWords},
		{"arm64", f.ARM64, boolWords},
	} {
		if check.value != "" && !slices.Contains(check.allowed, check.value) {
			return microerror.Maskf(client.InvalidFlagError, "--%s must be %s, got %q", check.name, oneOf(check.allowed), check.value)
		}
	}
	if f.InactiveDays < 0 || f.Limit < 0 {
		return microerror.Maskf(client.InvalidFlagError, "--inactive-days and --limit must not be negative")
	}
	return nil
}

// args are the tool's arguments: the scope, and every filter that is set.
func (f *flag) args() map[string]any {
	args := map[string]any{"scope": f.Scope}
	set := func(key, value string) {
		if value != "" {
			args[key] = value
		}
	}
	set("team", f.Team)
	set("search", f.Search)
	set("renovate", f.Renovate)
	set("visibility", f.Visibility)
	set("lifecycle", f.Lifecycle)
	set("finding", f.Finding)
	set("orb", f.Orb)
	set("chinaPush", f.ChinaPush)
	set("signing", f.Signing)
	setBool := func(key, value string) {
		if value != "" {
			b, _ := strconv.ParseBool(value)
			args[key] = b
		}
	}
	setBool("fork", f.Fork)
	setBool("archived", f.Archived)
	setBool("undeclared", f.Undeclared)
	setBool("arm64", f.ARM64)
	if f.InactiveDays > 0 {
		args["inactiveDays"] = f.InactiveDays
	}
	if f.Limit > 0 {
		args["limit"] = f.Limit
	}
	return args
}

func oneOf(values []string) string {
	out := ""
	for i, v := range values {
		switch {
		case i == 0:
			out = v
		case i == len(values)-1:
			out += " or " + v
		default:
			out += ", " + v
		}
	}
	return out
}
