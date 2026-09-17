package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

func (h *harness) create(mode Mode) (*CreateResult, error) {
	h.t.Helper()
	return h.runner.Create(context.Background(), CreateRequest{Team: team, Entry: h.entry, Mode: mode})
}

func stepNames(steps []StepResult) []Step {
	names := make([]Step, 0, len(steps))
	for _, s := range steps {
		names = append(names, s.Step)
	}
	return names
}

// TestCreateDryRunWritesNothing: the check mode plans the creation and the
// scaffold of a repository that does not exist and touches nothing.
func TestCreateDryRunWritesNothing(t *testing.T) {
	h := newHarness(t, entryYAML)

	res, err := h.create(ModeCheck)
	require.NoError(t, err)
	require.Equal(t, []Step{StepCreate, StepScaffold}, stepNames(res.Steps))
	require.Equal(t, VerdictDrift, res.Step(StepCreate).Verdict)
	require.Equal(t, []string{"create giantswarm/sample-service from the added entry"}, res.Step(StepCreate).Changes)
	require.Equal(t, VerdictDrift, res.Step(StepScaffold).Verdict)
	require.Equal(t, []string{"render the scaffold and push it as the first commit on main"}, res.Step(StepScaffold).Changes)
	require.False(t, res.Created)
	require.Empty(t, res.URL)
	require.Empty(t, res.ScaffoldCommit)
	require.Empty(t, h.mutations(), "a dry run writes nothing")
	_, exists := h.gh.repos[owner+"/"+name]
	require.False(t, exists)
}

// TestCreateCreatesThenScaffolds: the repair creates the repository from the
// declaration and pushes the scaffold as the one commit on main, and
// returns the repository and the commit.
func TestCreateCreatesThenScaffolds(t *testing.T) {
	h := newHarness(t, entryYAML)

	res, err := h.create(ModeRepair)
	require.NoError(t, err)
	require.Equal(t, []Step{StepCreate, StepScaffold}, stepNames(res.Steps))
	require.Equal(t, VerdictRepaired, res.Step(StepCreate).Verdict)
	require.Equal(t, VerdictRepaired, res.Step(StepScaffold).Verdict)
	require.Empty(t, res.Failed())

	r := h.repo()
	require.Equal(t, "A sample service", r.description)
	require.False(t, r.private)
	require.Contains(t, r.files, "README.md")
	require.Contains(t, r.files, "CODEOWNERS", "the scaffold replaced the initial README")
	require.Equal(t, scaffoldSubject, r.headSubject("main"))

	require.True(t, res.Created)
	require.Equal(t, owner+"/"+name, res.Repository)
	require.Equal(t, "https://github.com/giantswarm/sample-service", res.URL)
	require.Equal(t, r.heads["main"], res.ScaffoldCommit, "the scaffold commit is the head of main")
	require.NotEmpty(t, res.ScaffoldCommit)

	// The order is create, then scaffold: the creation is the first write.
	require.Equal(t, "POST /orgs/giantswarm/repos", h.gh.mutations[0])
}

// TestCreateResumesAnExistingRepository: a repository that exists is not
// created again; a scaffold that is there is not pushed again, and a
// repository left without one (a creation interrupted before the scaffold)
// gets it.
func TestCreateResumesAnExistingRepository(t *testing.T) {
	t.Run("with the scaffold", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.addRepo(owner, name)

		res, err := h.create(ModeRepair)
		require.NoError(t, err)
		require.Equal(t, VerdictOK, res.Step(StepCreate).Verdict)
		require.Equal(t, "exists", res.Step(StepCreate).Summary)
		require.Equal(t, "present", res.Step(StepScaffold).Summary)
		require.False(t, res.Created)
		require.Equal(t, "https://github.com/giantswarm/sample-service", res.URL)
		require.Equal(t, "head", res.ScaffoldCommit, "the commit found at the head of main")
		require.Empty(t, h.mutations(), "nothing to do")
	})
	t.Run("without the scaffold", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := h.gh.addRepo(owner, name)
		r.files = map[string]string{"README.md": "# sample-service\n"}

		res, err := h.create(ModeRepair)
		require.NoError(t, err)
		require.Equal(t, VerdictOK, res.Step(StepCreate).Verdict)
		require.Equal(t, VerdictRepaired, res.Step(StepScaffold).Verdict)
		require.False(t, res.Created)
		require.Equal(t, r.heads["main"], res.ScaffoldCommit)
		require.NotContains(t, h.mutations(), "POST /orgs/giantswarm/repos")
	})
}

// TestCreateRefusesANonOwner: the caller's role is read before any write;
// a member who is not an owner is refused with the text that names the way
// out, and a 403 on the creation itself is the same refusal.
func TestCreateRefusesANonOwner(t *testing.T) {
	want := "only an organization owner may create a repository in giantswarm — ask an owner, or create it from the Dev Portal (which also creates it as you and needs the same role)"
	require.Equal(t, want, NotOwnerRefusal(owner))

	for _, role := range []string{"member", ""} {
		t.Run("role "+role, func(t *testing.T) {
			h := newHarness(t, entryYAML)
			h.gh.orgRole = role
			for _, mode := range []Mode{ModeCheck, ModeRepair} {
				res, err := h.create(mode)
				require.Nil(t, res)
				require.True(t, IsNotOwner(err), "%v", err)
				require.Contains(t, err.Error(), want)
			}
			require.Empty(t, h.mutations())
			_, exists := h.gh.repos[owner+"/"+name]
			require.False(t, exists)
		})
	}

	t.Run("403 on the creation", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.createStatus = 403

		res, err := h.create(ModeRepair)
		require.NoError(t, err)
		require.Equal(t, VerdictFailed, res.Step(StepCreate).Verdict)
		require.Contains(t, res.Step(StepCreate).Summary, want)
		require.Nil(t, res.Step(StepScaffold), "nothing to scaffold")
		require.Len(t, res.Failed(), 1)
		require.False(t, res.Created)
	})
}

// TestCreateRefusesWhatCannotRun: the checks every run makes, plus the
// renderer the scaffold needs.
func TestCreateRefusesWhatCannotRun(t *testing.T) {
	h := newHarness(t, entryYAML)

	_, err := h.runner.Create(context.Background(), CreateRequest{Team: team, Entry: reposetup.Entry{Name: name}})
	require.True(t, IsInvalidConfig(err), "an unaccepted entry: %v", err)

	_, err = h.runner.Create(context.Background(), CreateRequest{Entry: h.entry})
	require.True(t, IsInvalidConfig(err), "no team: %v", err)

	h.runner.Renderer = nil
	_, err = h.create(ModeRepair)
	require.True(t, IsInvalidConfig(err), "no renderer in repair mode: %v", err)
	_, err = h.create(ModeCheck)
	require.NoError(t, err, "a dry run renders nothing")
	require.Empty(t, h.mutations())
}
