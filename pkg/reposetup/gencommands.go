package reposetup

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"

	gencmd "github.com/giantswarm/devctl/v8/cmd/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// The generators and the release workflows, as `devctl gen` names them.
const (
	genMakefile  = "makefile"
	genWorkflows = "workflows"
	genLLM       = "llm"
	genPrecommit = "precommit"
	genCircleCI  = "circleci"
	genRenovate  = "renovate"

	releaseWorkflowLegacy      = "legacy"
	releaseWorkflowAutoRelease = "auto-release"

	visibilityPrivate   = "private"
	lifecycleDeprecated = "deprecated"
	precommitHelmchart  = "helmchart"

	flagFlavour  = "--flavour"
	flagLanguage = "--language"
	flagRepoName = "--repo-name"
)

// genContext is what the command lines depend on besides the declaration:
// repository content align-files probes (a helm directory) and the flags
// the running devctl knows.
type genContext struct {
	// HasHelm says whether the scaffold has a helm/ directory: the default
	// pre-commit flavour is helmchart then.
	HasHelm bool
	// Knows says whether a `devctl gen <generator>` knows a flag. align-files
	// probes `--help` before passing a flag its pinned devctl may predate;
	// nil means every flag is known.
	Knows func(generator, flag string) bool
}

// genCommands returns the `devctl gen …` command lines align-files runs for
// a declaration, in align-files' order: makefile, workflows, llm,
// pre-commit, CircleCI when gen.ci.generate is on, then Renovate. Each
// line is an argv starting with devctl gen. The declaration has to have
// gen.flavours and gen.language; the validator refuses one that has not. A
// fork line gets nothing generated ([generates]): no line.
func genCommands(f Fields, gc genContext) [][]string {
	g := f.Gen
	if g == nil || len(g.Flavours) == 0 || g.Language == "" || !generates(g.Flavours) {
		return nil
	}
	knows := gc.Knows
	if knows == nil {
		knows = func(string, string) bool { return true }
	}

	flavour := strings.Join(g.Flavours, ",")
	ci := g.CI
	generateCI := ci != nil && ci.Generate != nil && *ci.Generate

	line := func(generator string, args ...string) []string {
		return append([]string{"devctl", "gen", generator}, args...)
	}
	var commands [][]string

	commands = append(commands, line(genMakefile, flagFlavour, flavour, flagLanguage, g.Language))

	// Workflows. The release workflow follows the CI surface: generated CI
	// implies auto-release, a repository without it stays on legacy. The
	// OpenSSF scorecard runs on public repositories unless switched off.
	// --repo-name is what a scaffold render always needs for cliff.toml: its
	// temporary directory has no origin remote to read the name from.
	workflows := []string{flagFlavour, flavour, flagLanguage, g.Language, flagRepoName, f.Name}
	if g.InstallUpdateChart {
		workflows = append(workflows, "--install-update-chart")
	}
	if g.HelmDocsRegen {
		workflows = append(workflows, "--helm-docs-regen")
	}
	scorecard := (g.RunSecurityScorecard == nil || *g.RunSecurityScorecard) && f.Visibility != visibilityPrivate
	if !scorecard {
		workflows = append(workflows, "--run-security-scorecard=false")
	}
	if g.EnableUpstreamSyncAutomation {
		workflows = append(workflows, "--upstream-sync-automation")
	}
	if g.DispatchUpdateChartEventsRepo != "" {
		workflows = append(workflows, "--dispatch-update-chart-events-repo", g.DispatchUpdateChartEventsRepo)
	}
	releaseWorkflow := releaseWorkflowLegacy
	if generateCI {
		releaseWorkflow = releaseWorkflowAutoRelease
	}
	if ci != nil && ci.ReleaseWorkflow != "" {
		releaseWorkflow = ci.ReleaseWorkflow
	}
	workflows = append(workflows, "--release-workflow", releaseWorkflow)
	commands = append(commands, line(genWorkflows, workflows...))

	if g.GenerateLlmRules == nil || *g.GenerateLlmRules {
		commands = append(commands, line(genLLM, flagFlavour, flavour, flagLanguage, g.Language))
	}

	// Pre-commit: generated for every repository on generated CI, and for
	// one that lists gen.preCommit flavours; a repository still on the
	// static replace.precommit workflow keeps that one alone.
	staticPrecommit := f.Replace != nil && f.Replace.Precommit
	if g.PreCommit != nil || (generateCI && !staticPrecommit) {
		precommit := []string{flagLanguage, g.Language}
		if g.Language != "go" {
			// For Go, devctl reads the module path from go.mod.
			precommit = append(precommit, flagRepoName, f.Name)
		}
		flavors := g.PreCommit
		if len(flavors) == 0 && gc.HasHelm {
			flavors = []string{precommitHelmchart}
		}
		if len(flavors) > 0 {
			precommit = append(precommit, "--flavors", strings.Join(flavors, ","))
		}
		if g.GoGenerate && g.Language == "go" {
			precommit = append(precommit, "--go-generate")
		}
		commands = append(commands, line(genPrecommit, precommit...))
	}

	if generateCI {
		commands = append(commands, line(genCircleCI, circleCIArgs(f, flavour, knows)...))
	}

	// Renovate: every repository that generates at all gets its
	// configuration. On generated CI the architect orb is pinned by the
	// generated config, so --circleci-generated keeps Renovate off it; without
	// generated CI there is no orb to leave alone. --repo-name is what
	// align-files' devctl reads from the checkout's origin remote.
	renovate := []string{flagLanguage, g.Language}
	if generateCI {
		renovate = append(renovate, "--circleci-generated")
	}
	renovate = append(renovate, flagRepoName, f.Name)
	if f.Lifecycle == lifecycleDeprecated {
		renovate = append(renovate, "--deprecated")
	}
	for _, reviewer := range f.ChoreReviewers {
		renovate = append(renovate, "-r", reviewer)
	}
	commands = append(commands, line(genRenovate, renovate...))

	return commands
}

// circleCIArgs are the arguments of the `devctl gen circleci` line of a
// declaration on generated CI: every gen.ci knob is a flag; a flag this
// devctl does not know is left out, as align-files leaves it out after its
// probe.
func circleCIArgs(f Fields, flavour string, knows func(generator, flag string) bool) []string {
	g, ci := f.Gen, f.Gen.CI
	args := []string{flagRepoName, f.Name, flagLanguage, g.Language, flagFlavour, flavour}
	add := func(flag string, values ...string) {
		if knows(genCircleCI, flag) {
			args = append(append(args, flag), values...)
		}
	}
	if ci.AppCatalog != "" {
		add("--app-catalog", ci.AppCatalog)
	}
	if ci.AppCatalogTest != "" {
		add("--app-catalog-test", ci.AppCatalogTest)
	}
	if ci.ChartName != "" {
		add("--chart-name", ci.ChartName)
	}
	if ci.ChartReleaseGateJob != "" {
		add("--chart-release-gate-job", ci.ChartReleaseGateJob)
	}
	if ci.ForcePublic {
		add("--force-public")
	}
	if ci.OverrideChartAppVersion != nil {
		add("--override-chart-app-version=" + strconv.FormatBool(*ci.OverrideChartAppVersion))
	}
	if img := ci.Image; img != nil {
		if img.PreBuildJob != "" {
			add("--image-pre-build-job", img.PreBuildJob)
		}
		if img.PrivateOnly {
			add("--image-private-only")
		}
		if img.Name != "" {
			add("--image-name", img.Name)
		}
		if img.Platforms != "" {
			add("--image-platforms", img.Platforms)
		}
		if img.NativeBuilds {
			add("--image-native-builds")
		}
		platforms := make([]string, 0, len(img.ResourceClasses))
		for platform := range img.ResourceClasses {
			platforms = append(platforms, platform)
		}
		sort.Strings(platforms)
		for _, platform := range platforms {
			add("--image-resource-class", platform+"="+img.ResourceClasses[platform])
		}
		if img.Dockerfile != "" {
			add("--image-dockerfile", img.Dockerfile)
		}
	}
	if ci.BranchPublish {
		add("--branch-publish")
	}
	if ci.SkipAppCatalog {
		add("--skip-app-catalog")
	}
	if ci.SkipATS {
		add("--skip-ats")
	}
	if ci.ATSVersion != "" {
		add("--ats-version", ci.ATSVersion)
	}
	if ci.ATSOnRelease {
		add("--ats-on-release")
	}
	if ci.ATSResourceClass != "" {
		add("--ats-resource-class", ci.ATSResourceClass)
	}
	if ci.BuildConcurrency != "" {
		add("--build-concurrency", ci.BuildConcurrency)
	}
	if ci.ResourceClass != "" {
		add("--resource-class", ci.ResourceClass)
	}
	if ci.Go != nil && ci.Go.TestArtifacts != "" {
		add("--go-test-artifacts", ci.Go.TestArtifacts)
	}
	if n := ci.Node; n != nil {
		if n.TestTarget != "" {
			add("--node-test-target", n.TestTarget)
		}
		if n.BuildTarget != "" {
			add("--node-build-target", n.BuildTarget)
		}
		if n.BuildOutput != "" {
			add("--node-build-output", n.BuildOutput)
		}
	}
	return args
}

// generates says whether the generators produce anything for the declared
// flavours, as [gen.FlavourSlice.Generates] says it for the parsed ones: a
// fork line carries its upstream's files plus the carried patches and gets
// nothing generated.
func generates(flavours []string) bool {
	fl := make(gen.FlavourSlice, len(flavours))
	for i, f := range flavours {
		fl[i] = gen.Flavour(f)
	}
	return fl.Generates()
}

// cwdMu serializes the generator runs: the generators read the repository
// from the process working directory, as `devctl gen` does, so the
// directory is changed for the duration of a run.
var cwdMu sync.Mutex

// runGen runs `devctl gen` command lines in dir through the same command
// tree the CLI executes, so the files are what align-files' devctl writes.
// The process working directory is dir while the commands run.
func runGen(ctx context.Context, dir string, log io.Writer, commands [][]string) error {
	if log == nil {
		log = io.Discard
	}
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		return microerror.Mask(err)
	}

	cwdMu.Lock()
	defer cwdMu.Unlock()

	wd, err := os.Getwd()
	if err != nil {
		return microerror.Mask(err)
	}
	if err := os.Chdir(dir); err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	for _, argv := range commands {
		// A fresh command tree per line: cobra keeps parsed flag values on
		// the command.
		root, err := gencmd.New(gencmd.Config{Logger: logger, Stdout: log, Stderr: log})
		if err != nil {
			return microerror.Mask(err)
		}
		root.SilenceUsage = true
		root.SilenceErrors = true
		root.SetArgs(argv[2:])
		if err := root.ExecuteContext(ctx); err != nil {
			return microerror.Maskf(renderFailedError, "%s: %v", strings.Join(argv, " "), err)
		}
	}

	return nil
}

// knownGenFlags reports the flags the running devctl's `gen` commands have,
// the in-process form of align-files' `devctl gen <generator> --help | grep`
// probe.
func knownGenFlags() (func(generator, flag string) bool, error) {
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	root, err := gencmd.New(gencmd.Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return func(generator, flag string) bool {
		sub, _, err := root.Find([]string{generator})
		if err != nil || sub == root {
			return false
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(flag, "--"), "=")
		return sub.Flags().Lookup(name) != nil
	}, nil
}

// commandLine renders an argv the way a shell would show it.
func commandLine(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " \t\"'") {
			a = fmt.Sprintf("%q", a)
		}
		quoted[i] = a
	}
	return strings.Join(quoted, " ")
}
