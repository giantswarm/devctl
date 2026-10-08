package merge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

// onDoneTimeout bounds the --on-done command.
const onDoneTimeout = 10 * time.Minute

// startDocument is the document of devctl pr merge --detach: the merge it
// started and how to read its outcome.
type startDocument struct {
	agentcli.Envelope
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	Identity   string `json:"identity"`
	// Handle names the detached merge; devctl pr merge status <handle>
	// reads it.
	Handle string `json:"handle"`
	// PID is the merge's process.
	PID int `json:"pid"`
	// Log is the file the merge writes its progress to.
	Log string `json:"log"`
	// Document is the file the merge's document lands in when it ended.
	Document string `json:"document"`
	// Status is the command that reads the outcome.
	Status string `json:"status"`
}

func (r *runner) newStartDocument() startDocument {
	return startDocument{Envelope: agentcli.NewEnvelope(command, time.Now())}
}

// detach checks the call as a blocking merge would, then starts that merge in
// a process of its own, in a session of its own, and returns its handle.
func (r *runner) detach(ctx context.Context, args []string, doc *startDocument) error {
	c, err := r.check(ctx, args, &doc.Repository, &doc.Number, &doc.Identity)
	if err != nil {
		return err
	}
	root, err := r.jobRoot()
	if err != nil {
		return err
	}
	running, ok, err := mergejob.Running(root, doc.Repository, c.number)
	if err != nil {
		return err
	}
	if ok {
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
			"a detached merge of %s#%d runs already (pid %d): devctl pr merge status %s", doc.Repository, c.number, running.PID, running.Handle)
	}
	now := time.Now()
	if err := mergejob.Prune(root, now); err != nil {
		doc.Warn(fmt.Sprintf("removing detached merges older than %s: %v", mergejob.Retention, err))
	}
	job, err := mergejob.Create(root, c.owner, c.repo, c.number, now)
	if err != nil {
		return err
	}
	exe, err := r.executable()
	if err != nil {
		return fmt.Errorf("finding devctl's own executable: %w", err)
	}
	log, err := os.OpenFile(job.Log(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer log.Close() //nolint:errcheck // the child holds its own descriptor

	child := exec.Command(exe, r.flag.childArgs(doc.Repository, c.number, job.Handle)...) //nolint:gosec // devctl itself
	child.Stdout, child.Stderr = log, log
	mergejob.Detach(child)
	if err := child.Start(); err != nil {
		return fmt.Errorf("starting the detached merge: %w", err)
	}
	job.PID = child.Process.Pid
	_ = child.Process.Release()
	if err := job.Save(); err != nil {
		return fmt.Errorf("recording the detached merge (pid %d runs on, its log is %s): %w", job.PID, job.Log(), err)
	}
	doc.Handle, doc.PID, doc.Log, doc.Document = job.Handle, job.PID, job.Log(), job.DocumentPath()
	doc.Status = "devctl pr merge status " + job.Handle
	return nil
}

// finishDetached reports the merge of a detached process: the document on
// stdout (its log) and into the job's directory, then the --on-done command.
func (r *runner) finishDetached(ctx context.Context, doc *document, mergeErr error) error {
	var buf bytes.Buffer
	err := agentcli.Report(io.MultiWriter(r.stdout, &buf), doc, agentcli.VerdictGreen, mergeErr)
	root, rerr := r.jobRoot()
	if rerr == nil {
		rerr = mergejob.WriteDocument(root, r.flag.DetachedHandle, buf.Bytes())
	}
	if rerr != nil {
		_, _ = fmt.Fprintf(r.stderr, "writing the document of %s: %v\n", r.flag.DetachedHandle, rerr)
		return err
	}
	if r.flag.OnDone != "" {
		o := r.onDone(ctx, root, doc)
		if werr := mergejob.WriteOnDone(root, r.flag.DetachedHandle, o); werr != nil {
			_, _ = fmt.Fprintf(r.stderr, "recording the --on-done outcome of %s: %v\n", r.flag.DetachedHandle, werr)
		}
	}
	return err
}

// onDone runs the --on-done command with the merge's outcome in its
// environment and its output in the log.
func (r *runner) onDone(ctx context.Context, root string, doc *document) mergejob.OnDone {
	o := mergejob.OnDone{Command: r.flag.OnDone}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), onDoneTimeout)
	defer cancel()
	argv := mergejob.Shell(r.flag.OnDone)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // the caller's own command is the purpose
	cmd.Env = append(os.Environ(),
		"DEVCTL_MERGE_HANDLE="+r.flag.DetachedHandle,
		"DEVCTL_MERGE_EXIT_CODE="+strconv.Itoa(doc.ExitCode),
		"DEVCTL_MERGE_DOCUMENT="+mergejob.Job{Dir: filepath.Join(root, r.flag.DetachedHandle)}.DocumentPath(),
		"DEVCTL_MERGE_REPOSITORY="+doc.Repository,
		"DEVCTL_MERGE_NUMBER="+strconv.Itoa(doc.Number),
	)
	cmd.Stdout, cmd.Stderr = r.stderr, r.stderr
	err := cmd.Run()
	o.ExitCode = -1
	if cmd.ProcessState != nil {
		o.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		o.Error = err.Error()
		_, _ = fmt.Fprintf(r.stderr, "--on-done %q: %v\n", r.flag.OnDone, err)
	}
	return o
}
