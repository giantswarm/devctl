package releasewait

import (
	"errors"
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// The release models: how a repository turns a merge into a tag.
const (
	// ReleaseModelAutoRelease tags the merge commit of every push to main
	// that warrants a bump, from conventional commits.
	ReleaseModelAutoRelease = "auto-release"
	// ReleaseModelLegacy tags from a release branch and a release pull
	// request; the tag is not on the merge commit of a feature pull request.
	ReleaseModelLegacy = "legacy"
)

// The CI models: where the names of the artifacts come from.
const (
	// CIModelGenerated: devctl renders .circleci/config.yml and workflows.yml
	// from the team-file entry; the entry names the artifacts.
	CIModelGenerated = "generated"
	// CIModelHandWritten: the repository maintains its CircleCI
	// configuration; its push jobs name the artifacts.
	CIModelHandWritten = "hand-written"
	// CIModelNone: no CircleCI; the tag is judged by its Actions runs.
	CIModelNone = "none"
)

// The artifact kinds.
const (
	KindImage        = "image"
	KindChart        = "chart"
	KindReleaseAsset = "release-asset"
)

// The artifact states.
const (
	StateAvailable = "available"
	StateMissing   = "missing"
)

// Artifact is one thing the release ships and where it stands.
type Artifact struct {
	// Kind is image, chart or release-asset.
	Kind string `json:"kind"`
	// Reference is the pullable reference (registry/name:tag) or, for a
	// release asset, its download URL.
	Reference string `json:"reference"`
	// Digest is set once the artifact is available.
	Digest string `json:"digest"`
	// State is available or missing.
	State string `json:"state"`

	// private selects the private registry and its keychain.
	private bool
	// chart and catalog, for a chart: what the catalog index lists it as.
	chart, catalog string
}

// Private says whether the artifact lives in the private registry.
func (a Artifact) Private() bool { return a.private }

// Pipeline is the tag pipeline on CircleCI as the document reports it.
type Pipeline struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	URL    string `json:"url"`
	// Workflows are the newest run of every workflow name.
	Workflows []PipelineWorkflow `json:"workflows"`
	// FailedJobs are workflow/job of every failed job when the tag's CI
	// failed; empty otherwise.
	FailedJobs []string `json:"failedJobs"`
}

// PipelineWorkflow is one workflow of the tag pipeline.
type PipelineWorkflow struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ActionsRun is one GitHub Actions run the tag triggered, for a repository
// without CircleCI.
type ActionsRun struct {
	Name       string `json:"name"`
	RunID      int64  `json:"runId"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

// Result is the command's document below the envelope.
type Result struct {
	Repository   string `json:"repository"`
	Tag          string `json:"tag"`
	SHA          string `json:"sha"`
	ReleaseModel string `json:"releaseModel"`
	CIModel      string `json:"ciModel"`
	// Artifacts are the expected images and charts, or the release assets
	// of a repository that ships neither.
	Artifacts []Artifact `json:"artifacts"`
	// Pipeline is the tag pipeline; null for a repository without CircleCI.
	Pipeline *Pipeline `json:"pipeline"`
	// Actions are the runs the tag triggered; empty with CircleCI.
	Actions []ActionsRun `json:"actions"`
}

// NewResult is the result before anything is known: the repository, empty
// arrays, no pipeline.
func NewResult(repository string) Result {
	return Result{Repository: repository, Artifacts: []Artifact{}, Actions: []ActionsRun{}}
}

// Document is the command's JSON: the envelope and the result.
type Document struct {
	agentcli.Envelope
	Result
}

func allAvailable(artifacts []Artifact) bool {
	for _, a := range artifacts {
		if a.State != StateAvailable {
			return false
		}
	}
	return len(artifacts) > 0
}

// The outcomes of the exit-code table this command produces.
func ciFailedErr(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictCIFailed, format, args...)
}

func timeoutErr(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, format, args...)
}

func notApplicableErr(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, format, args...)
}

func usageErr(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, format, args...)
}

// isOutcome says whether err already carries its exit code.
func isOutcome(err error) bool {
	var coder agentcli.ExitCoder
	return errors.As(err, &coder)
}

// RegistryAnswerError is a registry answer that is neither a digest nor
// "manifest unknown": the probe's failure, which ends the wait at once.
type RegistryAnswerError struct {
	Reference string
	Status    int
	Code      string
	Hint      string
}

func (e *RegistryAnswerError) Error() string {
	msg := fmt.Sprintf("the registry answered HTTP %d %s", e.Status, e.Code)
	if e.Hint != "" {
		msg += ": " + e.Hint
	}
	return msg
}
