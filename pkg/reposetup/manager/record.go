package manager

import (
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// The manager's answers as the repo commands read them. The manager's own
// types are the contract (giantswarm-repo-manager, internal/tools and
// internal/inventory; docs/inventory-record.md); the fields below are the
// ones devctl renders as text, and `-o json` prints the answer as it came,
// whatever these types know of it.

// Record is one inventory record: the declaration joined with the reality on
// GitHub, CircleCI, Renovate and the catalog, the set-up state and the
// findings.
type Record struct {
	// Repository is owner/name, Name the name alone.
	Repository string `json:"repository"`
	Name       string `json:"name,omitempty"`
	// Declaration is nil for an unassigned repository (no team file declares it).
	Declaration *Declaration `json:"declaration"`
	// Reality is nil for a repository gone from GitHub.
	Reality  *Reality       `json:"reality"`
	CircleCI *CircleCIFacts `json:"circleci,omitempty"`
	CI       *CIFacts       `json:"ci,omitempty"`
	Renovate *RenovateFacts `json:"renovate,omitempty"`
	Catalog  *Presence      `json:"catalog,omitempty"`
	Mapping  *Presence      `json:"mapping,omitempty"`
	Setup    Setup          `json:"setup"`
	Findings []Finding      `json:"findings,omitempty"`
	// RefreshedAt is when the record was built, Source by what (sweep,
	// refresh, reconciler), Age how long ago, filled on read.
	RefreshedAt time.Time `json:"refreshedAt,omitempty"`
	Source      string    `json:"source,omitempty"`
	Age         string    `json:"age,omitempty"`
}

// Declaration is the repository's team-file entry as the inventory read it.
type Declaration struct {
	Team          string   `json:"team"`
	File          string   `json:"file"`
	ComponentType string   `json:"componentType,omitempty"`
	Lifecycle     string   `json:"lifecycle,omitempty"`
	Language      string   `json:"language,omitempty"`
	Flavours      []string `json:"flavours,omitempty"`
	// Entry is the entry as the team file carries it, one YAML list item.
	Entry    string              `json:"entry,omitempty"`
	Accepted bool                `json:"accepted"`
	Problems []reposetup.Problem `json:"problems,omitempty"`
}

// Reality is what GitHub says.
type Reality struct {
	URL              string   `json:"url"`
	Description      string   `json:"description,omitempty"`
	Visibility       string   `json:"visibility,omitempty"`
	DefaultBranch    string   `json:"defaultBranch,omitempty"`
	IsArchived       bool     `json:"isArchived"`
	IsFork           bool     `json:"isFork"`
	IsEmpty          bool     `json:"isEmpty"`
	Language         string   `json:"language,omitempty"`
	LastCommit       *Commit  `json:"lastCommit,omitempty"`
	LastPersonCommit *Commit  `json:"lastPersonCommit,omitempty"`
	LatestRelease    *Release `json:"latestRelease,omitempty"`
	CodeownersTeams  []string `json:"codeownersTeams,omitempty"`
	OpenIssues       int      `json:"openIssues"`
}

// Commit is one commit of the sampled history.
type Commit struct {
	Date    time.Time `json:"date"`
	Author  string    `json:"author"`
	Message string    `json:"message,omitempty"`
}

// Release is the latest release and whether CircleCI built it.
type Release struct {
	Tag         string    `json:"tag"`
	PublishedAt time.Time `json:"publishedAt"`
	Build       *Statuses `json:"build,omitempty"`
}

// Statuses is the worst state of a commit's ci/circleci: statuses.
type Statuses struct {
	State    string   `json:"state"`
	Contexts []string `json:"contexts,omitempty"`
}

// CircleCIFacts is what the statuses and the reconciler's run say about
// CircleCI; no CircleCI token is involved.
type CircleCIFacts struct {
	Followed       *bool     `json:"followed,omitempty"`
	SetupWorkflows *bool     `json:"setupWorkflows,omitempty"`
	Head           *Statuses `json:"head,omitempty"`
	Source         string    `json:"source,omitempty"`
	Unknown        []string  `json:"unknown,omitempty"`
	Error          string    `json:"error,omitempty"`
}

// CIFacts is what the CircleCI configuration on the default branch says.
type CIFacts struct {
	Generated     bool     `json:"generated"`
	Orb           string   `json:"orb,omitempty"`
	ImagePush     bool     `json:"imagePush"`
	ChartPush     bool     `json:"chartPush"`
	Platforms     []string `json:"platforms,omitempty"`
	ARM64         *bool    `json:"arm64,omitempty"`
	ChinaPush     string   `json:"chinaPush,omitempty"`
	Signing       string   `json:"signing,omitempty"`
	SigningReason string   `json:"signingReason,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// RenovateFacts is the Renovate configuration and activity.
type RenovateFacts struct {
	Configured      bool        `json:"configured"`
	Path            string      `json:"path,omitempty"`
	Enabled         bool        `json:"enabled"`
	DashboardIssue  *Issue      `json:"dashboardIssue,omitempty"`
	LastPullRequest *RenovatePR `json:"lastPullRequest,omitempty"`
	LastCommit      *time.Time  `json:"lastCommit,omitempty"`
}

// Issue is an issue by number and title.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// RenovatePR is the newest open Renovate pull request.
type RenovatePR struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// Presence says whether the catalog or the mapping has the repository.
type Presence struct {
	Present bool   `json:"present"`
	Team    string `json:"team,omitempty"`
}

// Setup is the set-up state: the engine's checks in read mode, the last
// reconciler run and the run awaited.
type Setup struct {
	Checks     *reconcile.Result `json:"checks,omitempty"`
	CheckedAt  time.Time         `json:"checkedAt,omitempty"`
	CheckError string            `json:"checkError,omitempty"`
	LastRun    *LastRun          `json:"lastRun,omitempty"`
	PendingRun *PendingRun       `json:"pendingRun,omitempty"`
	MissingRun *MissingRun       `json:"missingRun,omitempty"`
}

// LastRun is the last reconciler run's artifact.
type LastRun struct {
	Result    reconcile.Result `json:"result"`
	RunURL    string           `json:"runUrl"`
	Timestamp time.Time        `json:"timestamp"`
	Change    *Change          `json:"change,omitempty"`
}

// Change is the change a run was for: created, added, transferred,
// archived, deleted, deprecated, changed, dispatched, nightly.
type Change struct {
	Kind        string             `json:"kind"`
	By          string             `json:"by,omitempty"`
	PullRequest *ChangePullRequest `json:"pullRequest,omitempty"`
	FromTeam    string             `json:"fromTeam,omitempty"`
}

// ChangePullRequest is the team-file pull request behind a change.
type ChangePullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// PendingRun is the reconciler run the record expects: an Align now, or the
// run of a team-file pull request once it merges.
type PendingRun struct {
	DispatchedAt   time.Time          `json:"dispatchedAt"`
	By             string             `json:"by"`
	Kind           string             `json:"kind,omitempty"`
	PullRequest    *ChangePullRequest `json:"pullRequest,omitempty"`
	MergedAt       *time.Time         `json:"mergedAt,omitempty"`
	ConflictsSince *time.Time         `json:"conflictsSince,omitempty"`
}

// MissingRun is an expected run given up: completed without a report, or
// never reported.
type MissingRun struct {
	DispatchedAt time.Time `json:"dispatchedAt"`
	By           string    `json:"by"`
	Kind         string    `json:"kind,omitempty"`
	NoticedAt    time.Time `json:"noticedAt"`
	RunsURL      string    `json:"runsUrl"`
	RunURL       string    `json:"runUrl,omitempty"`
	Conclusion   string    `json:"conclusion,omitempty"`
}

// Finding is one of the record's findings: the engine's, or the
// inventory's own (declared-but-gone, undeclared-on-github,
// reconcile-run-missing).
type Finding struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
	Source  string `json:"source,omitempty"`
}

// Listing is list_repositories' answer.
type Listing struct {
	Scope        string        `json:"scope"`
	Teams        []string      `json:"teams,omitempty"`
	TeamsSource  string        `json:"teamsSource,omitempty"`
	Note         string        `json:"note,omitempty"`
	Sweep        *SweepSummary `json:"sweep"`
	SweepRunning bool          `json:"sweepRunning"`
	Total        int           `json:"total"`
	Matched      int           `json:"matched"`
	Shown        int           `json:"shown"`
	Repositories []Row         `json:"repositories"`
}

// Row is one line of the listing.
type Row struct {
	Repository       string   `json:"repository"`
	Team             string   `json:"team,omitempty"`
	Lifecycle        string   `json:"lifecycle,omitempty"`
	Visibility       string   `json:"visibility,omitempty"`
	Archived         bool     `json:"archived"`
	Fork             bool     `json:"fork,omitempty"`
	Gone             bool     `json:"gone,omitempty"`
	Renovate         string   `json:"renovate,omitempty"`
	LastPersonCommit string   `json:"lastPersonCommit,omitempty"`
	Findings         []string `json:"findings,omitempty"`
	CI               *RowCI   `json:"ci,omitempty"`
	Setup            RowSetup `json:"setup"`
	Age              string   `json:"age"`
}

// RowCI is the CI facts of a row.
type RowCI struct {
	Orb       string `json:"orb,omitempty"`
	ARM64     *bool  `json:"arm64,omitempty"`
	ChinaPush string `json:"chinaPush"`
	Signing   string `json:"signing"`
}

// RowSetup is the set-up state of a row.
type RowSetup struct {
	Converged  *bool       `json:"converged,omitempty"`
	Refused    bool        `json:"refused,omitempty"`
	CheckedAt  string      `json:"checkedAt,omitempty"`
	LastRun    string      `json:"lastRun,omitempty"`
	PendingRun *PendingRun `json:"pendingRun,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// SweepSummary is the last inventory sweep.
type SweepSummary struct {
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt"`
	Duration     string    `json:"duration"`
	Repositories int       `json:"repositories"`
	Declared     int       `json:"declared"`
	Undeclared   int       `json:"undeclared"`
	Gone         int       `json:"gone"`
	Archived     int       `json:"archived"`
	EngineChecks int       `json:"engineChecks"`
	Errors       []string  `json:"errors,omitempty"`
}

// Sweep is sweep_inventory's answer.
type Sweep struct {
	Running bool          `json:"running"`
	Started bool          `json:"started"`
	Last    *SweepSummary `json:"last,omitempty"`
	Login   string        `json:"login"`
	Teams   []string      `json:"teams"`
}

// Info is get_info's answer: the service and how the call is authenticated.
type Info struct {
	Version    string  `json:"version"`
	ToolPrefix string  `json:"toolPrefix"`
	Caller     *Person `json:"caller"`
	Auth       struct {
		Mode                string `json:"mode"`
		AuthorizationServer string `json:"authorizationServer,omitempty"`
		Reason              string `json:"reason,omitempty"`
	} `json:"auth"`
	TeamFiles struct {
		Repository string `json:"repository"`
		Ref        string `json:"ref"`
		Readable   string `json:"readable"`
		Reason     string `json:"reason,omitempty"`
	} `json:"teamFiles"`
	Inventory struct {
		Identity  string `json:"identity"`
		Connected bool   `json:"connected"`
		Records   int    `json:"records"`
		Error     string `json:"error,omitempty"`
	} `json:"inventory"`
	CircleCI struct {
		Source string `json:"source"`
	} `json:"circleci"`
	Reviews struct {
		Configured   bool   `json:"configured"`
		DebugChannel string `json:"debugChannel,omitempty"`
	} `json:"reviews"`
	Engine struct {
		Module  string `json:"module"`
		Version string `json:"version"`
		Package string `json:"package"`
	} `json:"engine"`
	Capabilities struct {
		Modes        []string `json:"modes"`
		ApplyRefused bool     `json:"applyRefused"`
		WriteTools   []string `json:"writeTools"`
	} `json:"capabilities"`
}

// Person is who the manager sees calling: the GitHub login the person's
// token acts as.
type Person struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// Watch is watch_repository's answer: the phases of a new repository on its
// way to readiness.
type Watch struct {
	Repository    string              `json:"repository"`
	PullRequest   string              `json:"pullRequest,omitempty"`
	Phases        []Phase             `json:"phases"`
	Changed       bool                `json:"changed"`
	Ready         bool                `json:"ready"`
	Pending       string              `json:"pending,omitempty"`
	PendingReason string              `json:"pendingReason,omitempty"`
	Failure       *Failure            `json:"failure,omitempty"`
	Release       *WatchRelease       `json:"release,omitempty"`
	Findings      []reconcile.Finding `json:"findings,omitempty"`
	Waited        int                 `json:"waited"`
}

// Phase is one phase reached.
type Phase struct {
	Name    string    `json:"name"`
	At      time.Time `json:"at"`
	Seconds int       `json:"seconds"`
}

// Failure is the phase that failed and why.
type Failure struct {
	Phase  string `json:"phase"`
	Reason string `json:"reason"`
}

// WatchRelease is the first release.
type WatchRelease struct {
	Tag string `json:"tag"`
	URL string `json:"url"`
}

// Plan is a write's dry run: the entry before and after, the pull request
// as it would land, the ask and the notice that would follow.
type Plan struct {
	Repository  string              `json:"repository"`
	Team        string              `json:"team"`
	FromTeam    string              `json:"fromTeam,omitempty"`
	Before      string              `json:"before,omitempty"`
	Entry       string              `json:"entry,omitempty"`
	Problems    []reposetup.Problem `json:"problems,omitempty"`
	Accepted    bool                `json:"accepted"`
	PullRequest PlannedPullRequest  `json:"pullRequest"`
	Ask         *PlannedMessage     `json:"ask,omitempty"`
	Notice      *PlannedMessage     `json:"notice,omitempty"`
}

// PlannedPullRequest is the pull request before it exists.
type PlannedPullRequest struct {
	Repository string   `json:"repository"`
	Branch     string   `json:"branch"`
	Title      string   `json:"title"`
	Files      []string `json:"files"`
	Body       string   `json:"body"`
	As         string   `json:"as"`
}

// PlannedMessage is an ask or notice before it is posted.
type PlannedMessage struct {
	Team        string `json:"team"`
	Channel     string `json:"channel,omitempty"`
	Text        string `json:"text"`
	Deliverable bool   `json:"deliverable"`
	Reason      string `json:"reason,omitempty"`
}

// Committed is a write's outcome in mode commit.
type Committed struct {
	PullRequest *PullRequest `json:"pullRequest"`
	Ask         *Delivery    `json:"ask,omitempty"`
	Notice      *Delivery    `json:"notice,omitempty"`
	PendingRun  *PendingRun  `json:"pendingRun,omitempty"`
}

// PullRequest is a team-file pull request the manager opened as the person.
type PullRequest struct {
	Number    int    `json:"number"`
	URL       string `json:"url"`
	Branch    string `json:"branch"`
	Title     string `json:"title"`
	Author    string `json:"author,omitempty"`
	Existing  bool   `json:"existing,omitempty"`
	AutoMerge bool   `json:"autoMerge"`
}

// Delivery is what became of an ask or notice.
type Delivery struct {
	Team            string `json:"team"`
	Channel         string `json:"channel,omitempty"`
	IntendedChannel string `json:"intendedChannel,omitempty"`
	Delivered       bool   `json:"delivered"`
	ReviewID        string `json:"reviewId,omitempty"`
	Error           string `json:"error,omitempty"`
}

// Approval is approve_change's answer.
type Approval struct {
	PullRequest   int         `json:"pullRequest"`
	Team          string      `json:"team"`
	Author        string      `json:"author,omitempty"`
	Login         string      `json:"login"`
	Teams         []string    `json:"teams,omitempty"`
	Member        bool        `json:"member"`
	ReviewURL     string      `json:"reviewUrl,omitempty"`
	Merged        bool        `json:"merged"`
	AutoMerge     bool        `json:"autoMerge"`
	Rerendered    *Rerendered `json:"rerendered,omitempty"`
	RerenderError string      `json:"rerenderError,omitempty"`
	Message       string      `json:"message,omitempty"`
}

// Rerendered says the pull request was re-rendered on its base first.
type Rerendered struct {
	Number  int      `json:"number"`
	Branch  string   `json:"branch"`
	Base    string   `json:"base"`
	Commit  string   `json:"commit"`
	Files   []string `json:"files"`
	Entries []string `json:"entries"`
}

// Dispatch is align_repository's answer: the mode the entry decides, the
// warning, the planned changes and what was dispatched or opened.
type Dispatch struct {
	Workflow   string              `json:"workflow"`
	Inputs     map[string]any      `json:"inputs"`
	As         string              `json:"as"`
	Dispatched bool                `json:"dispatched"`
	RunsURL    string              `json:"runsUrl"`
	Then       string              `json:"then"`
	Findings   []reconcile.Finding `json:"findings,omitempty"`
	PendingRun *PendingRun         `json:"pendingRun,omitempty"`
	Team       string              `json:"team,omitempty"`
	Declared   bool                `json:"declared"`
	OptedIn    bool                `json:"optedIn"`
	Mode       string              `json:"mode"`
	OptIn      *OptIn              `json:"optIn,omitempty"`
	Planned    []PlannedStep       `json:"planned,omitempty"`
	CheckedAt  string              `json:"checkedAt,omitempty"`
	Warning    string              `json:"warning"`
}

// OptIn is the pull request that opts a repository in, planned and committed.
type OptIn struct {
	Plan      Plan       `json:"plan"`
	Committed *Committed `json:"committed,omitempty"`
}

// PlannedStep is one step's planned changes from the inventory's last check.
type PlannedStep struct {
	Step    string   `json:"step"`
	Changes []string `json:"changes"`
}
