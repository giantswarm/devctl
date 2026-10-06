// Package mergejob keeps the detached merges of devctl pr merge --detach: one
// directory per merge under the user's state directory, named by the merge's
// handle, holding the job, the merge's log and, once it ended, its document.
package mergejob

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// The files of a job's directory.
const (
	jobFile      = "job.json"
	documentFile = "document.json"
	onDoneFile   = "on-done.json"
	// LogFile is the merge's stdout and stderr: its --progress lines and
	// the document.
	LogFile = "output.log"
)

// The states of a job.
const (
	// StateRunning: the merge's process runs and has not written its
	// document.
	StateRunning = "running"
	// StateFinished: the merge wrote its document.
	StateFinished = "finished"
	// StateLost: the merge's process is gone without a document (killed,
	// the machine restarted); whether it merged is GitHub's to say.
	StateLost = "lost"
)

// Retention is how long a finished or lost job is kept; Start removes older
// ones.
const Retention = 30 * 24 * time.Hour

var handlePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Job is one detached merge.
type Job struct {
	Handle     string    `json:"handle"`
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"startedAt"`
	// Dir is the job's directory; not stored.
	Dir string `json:"-"`
}

// OnDone is how the --on-done command of a job ended.
type OnDone struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exitCode"`
	Error    string `json:"error,omitempty"`
}

// Status is what a job's directory says about it.
type Status struct {
	Job
	State string
	// Document is the merge's JSON document, set when finished.
	Document json.RawMessage
	// OnDone is the --on-done command's outcome, nil when none ran (yet).
	OnDone *OnDone
}

// Root is the directory the jobs live in: $XDG_STATE_HOME/devctl/merges, or
// ~/.local/state/devctl/merges without it.
func Root() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding the state directory for detached merges: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "devctl", "merges"), nil
}

// ValidHandle reports whether handle can name a job: no path separator, no
// leading dot.
func ValidHandle(handle string) bool { return handlePattern.MatchString(handle) }

// Create makes the directory of a new job for owner/repo#number started at
// now and returns the job; its handle is <owner>-<repo>-<number>-<UTC time>,
// with a suffix when a job of the same second exists.
func Create(root, owner, repo string, number int, now time.Time) (Job, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Job{}, fmt.Errorf("creating %s: %w", root, err)
	}
	base := fmt.Sprintf("%s-%s-%d-%s", owner, repo, number, now.UTC().Format("20060102T150405Z"))
	for i := 1; i < 100; i++ {
		handle := base
		if i > 1 {
			handle += "-" + strconv.Itoa(i)
		}
		dir := filepath.Join(root, handle)
		err := os.Mkdir(dir, 0o700)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return Job{}, fmt.Errorf("creating %s: %w", dir, err)
		}
		return Job{Handle: handle, Repository: owner + "/" + repo, Number: number, StartedAt: now.UTC(), Dir: dir}, nil
	}
	return Job{}, fmt.Errorf("no free handle for %s in %s", base, root)
}

// Save writes the job's record.
func (j Job) Save() error {
	return writeJSON(filepath.Join(j.Dir, jobFile), j)
}

// Log is the path of the job's log.
func (j Job) Log() string { return filepath.Join(j.Dir, LogFile) }

// DocumentPath is the path the merge's document is written to.
func (j Job) DocumentPath() string { return filepath.Join(j.Dir, documentFile) }

// WriteDocument stores the merge's document, atomically: a reader sees it
// whole or not at all.
func WriteDocument(root, handle string, document []byte) error {
	return writeFile(filepath.Join(root, handle, documentFile), document)
}

// WriteOnDone stores the --on-done command's outcome.
func WriteOnDone(root, handle string, o OnDone) error {
	return writeJSON(filepath.Join(root, handle, onDoneFile), o)
}

// Load reads the job named handle and judges its state.
func Load(root, handle string) (Status, error) {
	if !ValidHandle(handle) {
		return Status{}, fmt.Errorf("%q is not a handle of devctl pr merge --detach", handle)
	}
	dir := filepath.Join(root, handle)
	var s Status
	raw, err := os.ReadFile(filepath.Join(dir, jobFile)) //nolint:gosec // a validated handle under root
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, fmt.Errorf("no detached merge %s in %s", handle, root)
	}
	if err != nil {
		return Status{}, err
	}
	if err := json.Unmarshal(raw, &s.Job); err != nil {
		return Status{}, fmt.Errorf("reading %s: %w", filepath.Join(dir, jobFile), err)
	}
	s.Dir = dir
	if raw, err := os.ReadFile(filepath.Join(dir, onDoneFile)); err == nil { //nolint:gosec // a validated handle under root
		var o OnDone
		if json.Unmarshal(raw, &o) == nil {
			s.OnDone = &o
		}
	}
	switch doc, err := os.ReadFile(s.DocumentPath()); {
	case err == nil:
		s.State, s.Document = StateFinished, doc
	case !errors.Is(err, os.ErrNotExist):
		return Status{}, err
	case Alive(s.PID):
		s.State = StateRunning
	default:
		// The process may have written its document between the two reads.
		if doc, err := os.ReadFile(s.DocumentPath()); err == nil {
			s.State, s.Document = StateFinished, doc
		} else {
			s.State = StateLost
		}
	}
	return s, nil
}

// List reads every job under root, the newest first.
func List(root string) ([]Status, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Status{}, nil
	}
	if err != nil {
		return nil, err
	}
	jobs := []Status{}
	for _, e := range entries {
		if !e.IsDir() || !ValidHandle(e.Name()) {
			continue
		}
		s, err := Load(root, e.Name())
		if err != nil {
			continue // a directory being created, or not a job
		}
		jobs = append(jobs, s)
	}
	sort.SliceStable(jobs, func(a, b int) bool { return jobs[a].StartedAt.After(jobs[b].StartedAt) })
	return jobs, nil
}

// Running returns the running job of repository#number, if there is one.
func Running(root, repository string, number int) (Status, bool, error) {
	jobs, err := List(root)
	if err != nil {
		return Status{}, false, err
	}
	for _, s := range jobs {
		if s.Repository == repository && s.Number == number && s.State == StateRunning {
			return s, true, nil
		}
	}
	return Status{}, false, nil
}

// Prune removes the jobs that ended before now minus Retention.
func Prune(root string, now time.Time) error {
	jobs, err := List(root)
	if err != nil {
		return err
	}
	for _, s := range jobs {
		if s.State == StateRunning || now.Sub(s.StartedAt) < Retention {
			continue
		}
		if err := os.RemoveAll(s.Dir); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(b, '\n'))
}

func writeFile(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
