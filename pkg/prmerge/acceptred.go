package prmerge

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// securityScans are the scanners whose failure is never accepted: a check
// whose name contains one of them, in any case, is a security scan, and
// --accept-red naming it is refused.
var securityScans = []string{"nancy", "gitleaks", "govulncheck", "trivy", "kyverno", "codeql"}

// securityScan is the scanner the check name carries; empty for none.
func securityScan(name string) string {
	lower := strings.ToLower(name)
	for _, scan := range securityScans {
		if strings.Contains(lower, scan) {
			return scan
		}
	}
	return ""
}

// refuseAcceptRed refuses --accept-red before the wait (exit 3) for a name
// that is a security scan or that the base requires (branch protection or a
// ruleset). A name no check of the head carries is refused by the wait once
// the head is green: before, the check may not have reported yet.
func (m *Merger) refuseAcceptRed(ctx context.Context, owner, repo, baseRef string) error {
	if len(m.acceptRed) == 0 {
		return nil
	}
	for _, name := range m.acceptRed {
		if scan := securityScan(name); scan != "" {
			return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
				"--accept-red %s: a security scan (%s) is never accepted red; fix or bump what it found", name, scan)
		}
	}
	required, err := m.github.RequiredStatusContexts(ctx, owner, repo, baseRef)
	if err != nil {
		return microerror.Mask(err)
	}
	for _, name := range m.acceptRed {
		if slices.Contains(required, name) {
			return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
				"--accept-red %s: %s/%s requires it on %s (branch protection or a ruleset); a required check is never accepted red", name, owner, repo, baseRef)
		}
	}
	m.progress.Printf("accept-red: %s, neither required on %s nor a security scan", strings.Join(m.acceptRed, ", "), baseRef)
	return nil
}

// AcceptRedHelp is the usage of --accept-red; a caller detects the flag by
// it.
func AcceptRedHelp() string {
	return fmt.Sprintf("A check, status or CircleCI workflow, by its exact name, whose failure does not refuse the merge (repeatable); refused with exit 3 before the wait when the base requires it or it is a security scan (%s), and when the green head carries no check of that name", strings.Join(securityScans, ", "))
}
