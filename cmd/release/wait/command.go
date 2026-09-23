package wait

import (
	"io"
	"os"

	"github.com/spf13/cobra"
)

const (
	name        = "wait <owner/repo> [<vX.Y.Z|X.Y.Z>]"
	description = "Block until a tag's images and charts are pullable and its pipeline is green; one JSON document, exit codes for the outcome."
	long        = `Wait until the artifacts of a release are pullable and its tag pipeline is green.

The tag and the GitHub Release exist about a minute after a merge; the images
and charts come from the CircleCI pipeline the tag triggers, minutes later,
under the names the repository's CI decides. This command reads those names
from the sources that define them, never from the repository name:

  generated CI     the team-file entry of giantswarm/github, the way devctl's
                   generator renders it (gen.ci.image.name, gen.ci.chartName,
                   the app flavour, a Dockerfile at the tag)
  hand-written CI  the push jobs of the tag pipeline, matched with the
                   architect push jobs of the tag's .circleci configuration
  neither          a repository that ships release assets only is waited for
                   through its published release and the tag's workflows

The release model (auto-release or legacy) comes from the entry's
releaseWorkflow, defaulting from gen.ci.generate, cross-checked against the
workflow files at the tag; a disagreement is an error, never a guess. With
--pr the tag is the one auto-release put on the merge commit; a legacy
repository needs the version. The merge commit's auto-release run says when
no tag will come: a run that finished without one decided the commits
warrant no release (exit 3, verdict no_release), a run that failed before it
tagged is exit 1, a run cancelled before it tagged was superseded by a newer
push whose tag carries the merge (exit 3). devctl pr merge runs this wait
after its merge.

Availability is a digest and a green tag pipeline: every image and chart
resolves to one in its registry, and every workflow of the tag pipeline (the
newest run per workflow name) finished green. The names cover what devctl
renders and the orb pushes; a repository's own tag jobs (.circleci/custom.yml)
push and sign more, which only the finished pipeline vouches for. The public
registry is probed anonymously, so a stale docker login cannot produce a false
UNAUTHORIZED; the private registry is read with the docker keychain. An answer
other than a digest or "manifest unknown" ends the wait as a tooling failure.
A failed or cancelled workflow ends it as the tag's CI failure with the failed
jobs; a repository without CircleCI is judged by the Actions runs the tag
triggered. --catalog also waits for the catalog index to list the chart.

Output: one JSON document on stdout at the end and nothing else (--progress
writes one line per step to stderr): the envelope (command, schemaVersion,
exitCode, verdict, reason, warnings, startedAt, finishedAt), repository, tag,
sha, releaseModel, ciModel, artifacts[{kind, reference, digest, state}],
pipeline{id, number, url, workflows[{name, status}], failedJobs[], unfinished[]} (null
without CircleCI) and actions[{name, runId, status, conclusion, url}].

Exit codes:
  0  available: every artifact resolves to a digest and the tag pipeline
     is green
  1  the tag's CI failed, or with --pr the auto-release run failed before
     it tagged; the document names the failed jobs
  2  timeout; the document shows what is still missing or still running
  3  not applicable: the pull request is not merged; no release follows it
     (verdict no_release: the repository does not tag merge commits, or its
     auto-release run finished without a tag); or its auto-release run was
     superseded, so --pr cannot resolve a version
  7  usage or tooling: a bad argument, sources that disagree about the
     artifacts, a registry answer that is neither a digest nor "manifest
     unknown"
  8  authentication required: run ` + "`devctl auth login`" + `

Tokens come from the keychain (devctl auth login). GitHub is always needed;
CircleCI only when the tag carries a .circleci/config.yml.

Examples:
  devctl release wait giantswarm/devctl v8.9.0
  devctl release wait giantswarm/devctl --pr 2289 --timeout 20m --progress
  devctl release wait giantswarm/app-operator 7.5.4 --catalog`
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
		flag:   f,
		stderr: config.Stderr,
		stdout: config.Stdout,
		open:   openClients,
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  long,
		Args:  cobra.RangeArgs(1, 2),
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
