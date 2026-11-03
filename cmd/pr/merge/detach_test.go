package merge

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

// The test binary stands in for the detached devctl when this is set: it
// records its arguments and writes a green document for its handle into the
// root the variable names.
const childRootEnv = "DEVCTL_TEST_DETACHED_CHILD_ROOT"

func TestMain(m *testing.M) {
	if root := os.Getenv(childRootEnv); root != "" {
		os.Exit(fakeChild(root, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeChild(root string, args []string) int {
	var handle string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--"+flagDetachedHandle+"="); ok {
			handle = v
		}
	}
	if handle == "" {
		return 7
	}
	_ = os.WriteFile(filepath.Join(root, handle, "args"), []byte(strings.Join(args, "\n")), 0o600) //nolint:gosec // the test's own directory
	doc := `{"command":"pr merge","exitCode":0,"verdict":"green","reason":"","mergeCommitSha":"m1"}`
	if err := mergejob.WriteDocument(root, handle, []byte(doc)); err != nil {
		return 7
	}
	return 0
}

func detachRunner(t *testing.T, f *flag) (*runner, *bytes.Buffer, string) {
	t.Helper()
	r, stdout, _, _ := newRunner(t, sequence.Routes{}, loggedIn, f)
	root := t.TempDir()
	r.jobRoot = func() (string, error) { return root, nil }
	r.executable = func() (string, error) { return os.Args[0], nil }
	t.Setenv(childRootEnv, root)
	return r, stdout, root
}

func statusOf(t *testing.T, root string, args ...string) (map[string]any, int) {
	t.Helper()
	stdout := &bytes.Buffer{}
	r := &statusRunner{stdout: stdout, jobRoot: func() (string, error) { return root, nil }}
	err := r.run(args)
	return decode(t, stdout), agentcli.Exit(err)
}

func TestDetachStartsAndStatusReadsTheOutcome(t *testing.T) {
	f := &flag{Timeout: 45 * time.Minute, ReleaseTimeout: 30 * time.Minute, Rebase: true, Detach: true, OnDone: "true"}
	r, stdout, root := detachRunner(t, f)

	started := time.Now()
	err := r.run(t.Context(), []string{"o/r", "42"})
	if code := agentcli.Exit(err); code != agentcli.ExitOK {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("--detach took %s", took)
	}
	doc := decode(t, stdout)
	handle, _ := doc["handle"].(string)
	if doc["verdict"] != "detached" || !strings.HasPrefix(handle, "o-r-42-") || doc["status"] != "devctl pr merge status "+handle || doc["pid"].(float64) <= 0 {
		t.Fatalf("start document: %v", doc)
	}

	var s mergejob.Status
	for deadline := time.Now().Add(20 * time.Second); ; {
		if s, err = mergejob.Load(root, handle); err != nil {
			t.Fatal(err)
		}
		if s.State != mergejob.StateRunning || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.State != mergejob.StateFinished {
		t.Fatalf("state %s, want finished", s.State)
	}
	args, err := os.ReadFile(filepath.Join(root, handle, "args")) //nolint:gosec // the test's own directory
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "merge", "o/r", "42", "--timeout=45m0s", "--release-timeout=30m0s", "--progress", "--detached-handle=" + handle, "--rebase", "--on-done=true"}
	if got := strings.Split(string(args), "\n"); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("child arguments\n got %q\nwant %q", got, want)
	}

	status, code := statusOf(t, root, handle)
	merge, _ := status["merge"].(map[string]any)
	if code != agentcli.ExitOK || status["state"] != "finished" || status["verdict"] != "green" || merge["mergeCommitSha"] != "m1" {
		t.Errorf("status: exit %d, %v", code, status)
	}
}

func TestDetachRefusesASecondMergeOfTheSamePullRequest(t *testing.T) {
	r, stdout, root := detachRunner(t, &flag{Timeout: time.Minute, ReleaseTimeout: time.Minute, Detach: true})
	j, err := mergejob.Create(root, "o", "r", 42, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	j.PID = os.Getpid()
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	err = r.run(t.Context(), []string{"o/r", "42"})
	doc := decode(t, stdout)
	if agentcli.Exit(err) != agentcli.ExitNotApplicable || !strings.Contains(doc["reason"].(string), j.Handle) {
		t.Errorf("exit %d: %v", agentcli.Exit(err), doc)
	}
}

func TestDetachChecksTheCallFirst(t *testing.T) {
	r, stdout, root := detachRunner(t, &flag{Timeout: time.Minute, ReleaseTimeout: time.Minute, Detach: true})
	err := r.run(t.Context(), []string{"o/r", "nope"})
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Errorf("exit %d: %s", agentcli.Exit(err), stdout)
	}
	if jobs, _ := mergejob.List(root); len(jobs) != 0 {
		t.Errorf("a refused call left jobs: %+v", jobs)
	}
}

func TestOnDoneNeedsDetach(t *testing.T) {
	r, stdout, _, _ := newRunner(t, sequence.Routes{}, loggedIn, &flag{Timeout: time.Minute, ReleaseTimeout: time.Minute, OnDone: "true"})
	err := r.run(t.Context(), []string{"o/r", "42"})
	doc := decode(t, stdout)
	if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(doc["reason"].(string), "--on-done needs --detach") {
		t.Errorf("exit %d: %v", agentcli.Exit(err), doc)
	}
}

func TestDetachedProcessWritesItsDocumentAndRunsOnDone(t *testing.T) {
	root := t.TempDir()
	j, err := mergejob.Create(root, "o", "r", 42, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "on-done")
	f := &flag{Timeout: time.Minute, ReleaseTimeout: time.Minute, DetachedHandle: j.Handle,
		OnDone: `echo "$DEVCTL_MERGE_HANDLE $DEVCTL_MERGE_EXIT_CODE $DEVCTL_MERGE_REPOSITORY $DEVCTL_MERGE_NUMBER $DEVCTL_MERGE_DOCUMENT" > ` + out}
	// A merge that ends at once: the version gate refuses.
	r, stdout, _, _ := newRunner(t, sequence.Routes{}, loggedIn, f)
	r.gate = func(bool) error {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "devctl 9.9.9 is released")
	}
	r.jobRoot = func() (string, error) { return root, nil }

	err = r.run(t.Context(), []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Fatalf("exit %d", agentcli.Exit(err))
	}
	written, err := os.ReadFile(j.DocumentPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, stdout.Bytes()) {
		t.Errorf("the job's document differs from stdout:\n%s\n%s", written, stdout)
	}
	got, err := os.ReadFile(out) //nolint:gosec // the test's own directory
	if err != nil {
		t.Fatal(err)
	}
	if want := j.Handle + " 7 o/r 42 " + j.DocumentPath() + "\n"; string(got) != want {
		t.Errorf("--on-done saw %q, want %q", got, want)
	}

	status, code := statusOf(t, root, j.Handle)
	onDone, _ := status["onDone"].(map[string]any)
	if code != agentcli.ExitUsage || status["state"] != "finished" || status["reason"] != "devctl 9.9.9 is released" || onDone["exitCode"] != float64(0) {
		t.Errorf("status: exit %d, %v", code, status)
	}
}

func TestStatusRunningLostAndList(t *testing.T) {
	root := t.TempDir()
	running, _ := mergejob.Create(root, "o", "r", 1, time.Now())
	running.PID = os.Getpid()
	lost, _ := mergejob.Create(root, "o", "r", 2, time.Now().Add(time.Second))
	dead := exec.Command("go", "version")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	lost.PID = dead.Process.Pid
	for _, j := range []mergejob.Job{running, lost} {
		if err := j.Save(); err != nil {
			t.Fatal(err)
		}
	}

	if doc, code := statusOf(t, root, running.Handle); code != agentcli.ExitRunning || doc["verdict"] != "running" || doc["merge"] != nil {
		t.Errorf("running: exit %d, %v", code, doc)
	}
	if doc, code := statusOf(t, root, lost.Handle); code != agentcli.ExitUsage || doc["state"] != "lost" {
		t.Errorf("lost: exit %d, %v", code, doc)
	}
	if doc, code := statusOf(t, root, "nope"); code != agentcli.ExitUsage {
		t.Errorf("unknown handle: exit %d, %v", code, doc)
	}
	doc, code := statusOf(t, root)
	jobs, _ := doc["jobs"].([]any)
	if code != agentcli.ExitOK || len(jobs) != 2 || jobs[0].(map[string]any)["handle"] != lost.Handle {
		t.Errorf("list: exit %d, %v", code, doc)
	}
}
