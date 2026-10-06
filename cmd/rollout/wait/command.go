package wait

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

const (
	name        = "wait <installation> <owner/repo> [<vX.Y.Z|X.Y.Z>]"
	description = "Block until a release's charts run on an installation; one JSON document, exit codes for the outcome."
	long        = `Wait until a release runs on an installation.

The wait has two parts. The release first, exactly as devctl release wait
does it (a version, or --pr for the tag auto-release put on the merge
commit): its images and charts pullable, its tag pipeline green. The
release names the charts, a second chart its custom.yml pushes off the same
tag included. Then the installation's management cluster,
read through the kube context tsh kube login writes
(teleport.giantswarm.io-<installation>), as you. An installation reached
through another context (kubectl gs login's gs-<installation>, a kind lab)
is read through the one --context names. The context is checked before the
release wait: one the kubeconfig does not have is exit 7 at once, the
contexts that mention the installation named as candidates. It reads:

  HelmRelease  every Flux HelmRelease whose chart source (an OCIRepository
               whose URL ends in the chart's name, a HelmChart, or
               spec.chart) serves one of the charts
  App          every App CR whose spec.name is one of the charts

and every HelmRelease --helmrelease names (<name> or <namespace>/<name>,
repeatable) whatever chart it deploys; a name no HelmRelease answers to is
exit 3.

A deployment has rolled out when it runs the version or a newer one
(status.history of the HelmRelease, status.version and a deployed release
of the App), is Ready at its current generation, and every Deployment,
StatefulSet and DaemonSet of its Helm release (Helm's release annotations,
in the release namespace) is rolled out the way kubectl rollout status
judges it. The workloads of a HelmRelease with spec.kubeConfig or an App
into a workload cluster are not read; a warning says so.

A deployment that will not get there is reported in the document and a
warning, and the wait goes on for the others: its source pins another tag
or a digest, its semver range or the App's spec.version excludes the
version (a workload cluster's App pinned to an older release), or the
HelmRelease is suspended; when no deployment follows the version, exit 3.
The wait ends at once when the version was attempted and failed: the
HelmRelease Stalled, or not Ready after a failed install, upgrade or test
of the version, the App's release failed, a Deployment past its progress
deadline (exit 1).

The cluster is polled with a backoff from 5 to 30 seconds; --reconcile asks
Flux once to reconcile the sources and HelmReleases still behind, the only
write the command makes.

Output: one JSON document on stdout at the end and nothing else (--progress
writes one line per step to stderr): the envelope (command, schemaVersion,
exitCode, verdict, reason, warnings, startedAt, finishedAt), installation,
context, version, charts, deployments[{kind, namespace, name, chart,
source, follows, runningVersion, state, message, workloads[{kind,
namespace, name, ready, message}]}] and release, the release wait's
{verdict, reason} and its result.

Exit codes:
  0  rolled out: every deployment of the charts that follows the version
     runs it or a newer one
  1  the release's CI failed, or the rollout failed; the reason names the
     deployment and its condition
  2  timeout, of the release (--release-timeout) or of the rollout
     (--timeout); the document shows what is still missing
  3  not applicable: no release follows the pull request, the release
     ships no chart, nothing on the installation deploys its charts, or
     no deployment follows the version
  7  usage or tooling: a bad argument, a kube context that does not exist
     (the candidates and --context named),
     a request the cluster refused
  8  authentication required: devctl auth login for GitHub, tsh kube login
     <installation> for the cluster

Examples:
  devctl rollout wait myinstallation giantswarm/app-operator v7.5.4
  devctl rollout wait myinstallation giantswarm/app-operator --pr 1234 --reconcile --progress`
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

	f := &flag{}

	r := &runner{
		gate:        versiongate.Check,
		flag:        f,
		stderr:      config.Stderr,
		stdout:      config.Stdout,
		openRelease: releasewait.OpenSources,
		openCluster: rolloutwait.OpenCluster,
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  long,
		// The arguments are checked by the runner: a usage error is exit 7
		// with a document, like every other outcome.
		Args:        cobra.ArbitraryArgs,
		RunE:        r.Run,
		Annotations: agentcli.AgentFacing(),
	}
	c.SetFlagErrorFunc(r.FlagError)

	f.Init(c)

	return c, nil
}
