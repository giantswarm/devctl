package mergejob

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// deadPID is the pid of a process that ran and ended.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("go", "version")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func create(t *testing.T, root string, number, pid int, started time.Time) Job {
	t.Helper()
	j, err := Create(root, "o", "r", number, started)
	if err != nil {
		t.Fatal(err)
	}
	j.PID = pid
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestStates(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 7, 9, 15, 0, 0, time.UTC)
	running := create(t, root, 1, os.Getpid(), now)
	lost := create(t, root, 2, deadPID(t), now.Add(time.Second))
	finished := create(t, root, 3, deadPID(t), now.Add(2*time.Second))
	if err := WriteDocument(root, finished.Handle, []byte(`{"exitCode":0}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteOnDone(root, finished.Handle, OnDone{Command: "true"}); err != nil {
		t.Fatal(err)
	}

	for job, want := range map[string]string{running.Handle: StateRunning, lost.Handle: StateLost, finished.Handle: StateFinished} {
		s, err := Load(root, job)
		if err != nil {
			t.Fatal(err)
		}
		if s.State != want {
			t.Errorf("%s: state %s, want %s", job, s.State, want)
		}
	}
	s, _ := Load(root, finished.Handle)
	if string(s.Document) != `{"exitCode":0}` || s.OnDone == nil || s.OnDone.Command != "true" {
		t.Errorf("finished job: document %s, onDone %+v", s.Document, s.OnDone)
	}

	jobs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 || jobs[0].Handle != finished.Handle || jobs[2].Handle != running.Handle {
		t.Errorf("List is not newest first: %+v", jobs)
	}
	if got, ok, _ := Running(root, "o/r", 1); !ok || got.Handle != running.Handle {
		t.Errorf("Running(o/r, 1) = %v, %v", got.Handle, ok)
	}
	if _, ok, _ := Running(root, "o/r", 2); ok {
		t.Error("a lost job counts as running")
	}

	if err := Prune(root, now.Add(Retention+time.Hour)); err != nil {
		t.Fatal(err)
	}
	jobs, _ = List(root)
	if len(jobs) != 1 || jobs[0].Handle != running.Handle {
		t.Errorf("Prune kept %+v, want only the running job", jobs)
	}
}

func TestCreateSameSecond(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 7, 9, 15, 0, 0, time.UTC)
	a := create(t, root, 1, 0, now)
	b := create(t, root, 1, 0, now)
	if a.Handle != "o-r-1-20261007T091500Z" || b.Handle != "o-r-1-20261007T091500Z-2" {
		t.Errorf("handles %s and %s", a.Handle, b.Handle)
	}
}

func TestLoadRefusesPaths(t *testing.T) {
	root := t.TempDir()
	for _, h := range []string{"", "..", "../x", "a/b", ".hidden"} {
		if _, err := Load(root, h); err == nil {
			t.Errorf("Load(%q) did not refuse", h)
		}
	}
	if _, err := Load(root, "o-r-1-20261007T091500Z"); err == nil {
		t.Error("Load of an unknown handle did not fail")
	}
}
