// Package laptop is the wiring of the agent-facing engines on a laptop: the
// tokens from the keychain, a management cluster from a kubeconfig and tsh.
// The engines take the clients it builds; a server builds them from the
// tokens it holds for a person and never calls this package.
package laptop

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

// ClusterConfig names the management cluster OpenCluster opens.
type ClusterConfig struct {
	Installation string
	// Context is the kube context, [rolloutwait.ContextPrefix]+Installation by
	// default.
	Context string
	// Kubeconfig is the one file the context is read from; empty is the
	// default loading rules (KUBECONFIG, ~/.kube/config).
	Kubeconfig string
	// Wrap wraps the transport of every request.
	Wrap func(http.RoundTripper) http.RoundTripper
	// Teleport logs in when the kubeconfig lacks the Teleport context; nil
	// never logs in.
	Teleport Teleport
	// Progress is told about a login.
	Progress *agentcli.Progress
}

// OpenCluster opens the kube context of c with the requests going through
// c.Wrap. It reads the kubeconfig only, the cluster is not contacted.
//
// When the kubeconfig lacks the Teleport context ([rolloutwait.ContextPrefix] +
// installation) and tsh is logged in, tsh kube login writes the context into
// a private temporary kubeconfig, which is read and removed again: neither
// the default kubeconfig nor --kubeconfig is written. A tsh that is not
// logged in is exit 8. Any other context that does not exist, or one tsh is
// not installed to write, is a usage error naming the contexts that mention
// the installation, --context and tsh kube login.
func OpenCluster(ctx context.Context, c ClusterConfig) (dynamic.Interface, error) {
	rules := loadingRules(c.Kubeconfig)
	raw, err := rules.Load()
	if err != nil {
		return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "loading the kubeconfig: %v", err)
	}
	if _, ok := raw.Contexts[c.Context]; !ok {
		missing := MissingContextError(c.Installation, c.Context, slices.Collect(maps.Keys(raw.Contexts)))
		if c.Teleport == nil || c.Context != rolloutwait.ContextPrefix+c.Installation {
			return nil, missing
		}
		return openWithTeleport(ctx, c, missing)
	}
	return openContext(rules, c)
}

// openWithTeleport logs in to the installation into a private temporary
// kubeconfig and opens the context from it; the file is gone on return, the
// client keeps what it needs in memory (tsh's credential plugin runs per
// request without the file).
func openWithTeleport(ctx context.Context, c ClusterConfig, missing error) (dynamic.Interface, error) {
	loggedIn, why, err := c.Teleport.LoggedIn(ctx)
	if errors.Is(err, ErrNoTsh) {
		return nil, missing
	}
	if err != nil {
		return nil, fmt.Errorf("kube context %s does not exist, and %w", c.Context, err)
	}
	if !loggedIn {
		return nil, agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "kube context %s does not exist and tsh is not logged in (%s): sign in with `tsh login`, the wait then logs in to %s into a private kubeconfig; %s", c.Context, why, c.Installation, otherContexts)
	}

	dir, err := os.MkdirTemp("", "devctl-rollout-wait-")
	if err != nil {
		return nil, fmt.Errorf("creating the private kubeconfig: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	private := filepath.Join(dir, "config")

	c.Progress.Printf("kube context %s is not in the kubeconfig; tsh kube login %s into a private kubeconfig", c.Context, c.Installation)
	if err := c.Teleport.KubeLogin(ctx, c.Installation, c.Context, private); err != nil {
		return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "kube context %s does not exist and %v; %s", c.Context, err, otherContexts)
	}
	rules := loadingRules(private)
	raw, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("loading the kubeconfig tsh kube login %s wrote: %w", c.Installation, err)
	}
	if _, ok := raw.Contexts[c.Context]; !ok {
		return nil, fmt.Errorf("tsh kube login %s wrote no context %s", c.Installation, c.Context)
	}
	return openContext(rules, c)
}

// loadingRules reads the one file kubeconfig names, or the default rules.
func loadingRules(kubeconfig string) *clientcmd.ClientConfigLoadingRules {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	return rules
}

// openContext builds the client of c.Context from rules.
func openContext(rules *clientcmd.ClientConfigLoadingRules, c ClusterConfig) (dynamic.Interface, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: c.Context}).ClientConfig()
	if err != nil {
		return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "kube context %s: %v; `tsh kube login %s` writes it, --context names another", c.Context, err, c.Installation)
	}
	client, err := rolloutwait.NewClient(config, c.Wrap)
	if err != nil {
		return nil, fmt.Errorf("kube context %s: %w", c.Context, err)
	}
	return client, nil
}

// otherContexts names the flags that read an installation whose context is
// not in the default kubeconfig.
const otherContexts = "--kubeconfig <file> reads the context from a kubeconfig outside KUBECONFIG and ~/.kube/config, --context <name> reads the installation through another context"

// MissingContextError is the usage error for a kube context the kubeconfig
// does not have. An installation reached through another context than the
// one tsh kube login writes (kubectl gs login's gs-<installation>, a lab's
// own) is named with --context; the kubeconfig's contexts that mention the
// installation are listed as the candidates.
func MissingContextError(installation, kubeContext string, contexts []string) error {
	var candidates []string
	for _, c := range contexts {
		if strings.Contains(c, installation) {
			candidates = append(candidates, c)
		}
	}
	slices.Sort(candidates)
	hint := "no context in the kubeconfig mentions " + installation
	if len(candidates) > 0 {
		hint = "contexts in the kubeconfig that mention " + installation + ": " + strings.Join(candidates, ", ")
	}
	return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "kube context %s does not exist (%s); %s, `tsh kube login %s` writes this one", kubeContext, hint, otherContexts, installation)
}
