// Package agentcli is what devctl's agent-facing commands share: the JSON
// envelope every one of them prints as its only stdout output, the exit-code
// table, the clock that DEVCTL_TIME_SCALE speeds up for tests, the endpoint
// configuration read from the environment, the --progress writer and the
// retrying transport under the API clients.
//
// An agent-facing command blocks, prints one JSON document on stdout when it
// finishes and nothing else, and exits with a code from the table below.
// Progress, when asked for with --progress, goes to stderr.
package agentcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// SchemaVersion is the version of the envelope; a breaking change to any
// command's document bumps it.
const SchemaVersion = 1

// Verdict is the one-word outcome of a command.
type Verdict string

// The verdicts a command reports.
const (
	VerdictGreen              Verdict = "green"
	VerdictRed                Verdict = "red"
	VerdictTimeout            Verdict = "timeout"
	VerdictNotApplicable      Verdict = "not_applicable"
	VerdictRequiredMissing    Verdict = "required_missing"
	VerdictRefused            Verdict = "refused"
	VerdictAvailable          Verdict = "available"
	VerdictCIFailed           Verdict = "ci_failed"
	VerdictNoRelease          Verdict = "no_release"
	VerdictReleaseFailed      Verdict = "release_failed"
	VerdictReleaseUnconfirmed Verdict = "release_unconfirmed"
	VerdictAuthRequired       Verdict = "auth_required"
	VerdictUsage              Verdict = "usage"
)

// The exit codes of every agent-facing command. 6 and 9 say that devctl pr
// merge merged: they are never a reason to merge again.
const (
	// ExitOK: the wait ended green, the merge happened, the release is
	// available or none follows the merge.
	ExitOK = 0
	// ExitRed: a check is red or the tag's CI failed.
	ExitRed = 1
	// ExitTimeout: the deadline passed; the document names what was unfinished.
	ExitTimeout = 2
	// ExitNotApplicable: draft, closed, conflicting, behind a strict base, a
	// version that does not resolve.
	ExitNotApplicable = 3
	// ExitRequiredMissing: a required context never reported.
	ExitRequiredMissing = 4
	// ExitRefused: the command declines (another author, an opt-out).
	ExitRefused = 5
	// ExitReleaseFailed: merged, and the release the merge triggered failed:
	// its auto-release run or its tag's CI.
	ExitReleaseFailed = 6
	// ExitUsage: wrong usage or a tooling failure.
	ExitUsage = 7
	// ExitAuthRequired: no usable token; the reason names `devctl auth login`.
	ExitAuthRequired = 8
	// ExitReleaseUnconfirmed: merged, and the release was not confirmed
	// pullable: the release timeout passed first, or the release wait could
	// not judge it.
	ExitReleaseUnconfirmed = 9
)

// Envelope is the head of every command's JSON document. A command's document
// embeds it and adds its own fields.
type Envelope struct {
	Command       string    `json:"command"`
	SchemaVersion int       `json:"schemaVersion"`
	ExitCode      int       `json:"exitCode"`
	Verdict       Verdict   `json:"verdict"`
	Reason        string    `json:"reason"`
	Warnings      []string  `json:"warnings"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
}

// NewEnvelope starts the envelope of command at now.
func NewEnvelope(command string, now time.Time) Envelope {
	return Envelope{
		Command:       command,
		SchemaVersion: SchemaVersion,
		Warnings:      []string{},
		StartedAt:     now.UTC(),
	}
}

// Warn appends a warning; an empty message is ignored.
func (e *Envelope) Warn(message string) {
	if message == "" {
		return
	}
	e.Warnings = append(e.Warnings, message)
}

// Finish completes the envelope at now from the command's error: nil is exit
// 0 with the ok verdict, an [ExitCoder] carries its own code and verdict, and
// anything else is a tooling failure ([ExitUsage]).
func (e *Envelope) Finish(now time.Time, ok Verdict, err error) {
	e.FinishedAt = now.UTC()
	if err == nil {
		e.ExitCode = ExitOK
		e.Verdict = ok
		e.Reason = ""
		return
	}
	e.ExitCode, e.Verdict = Outcome(err)
	e.Reason = err.Error()
}

// Err is the error a command returns to cobra after emitting its document:
// nil on exit 0, otherwise an [*ExitError] carrying the envelope's code. The
// process exit code follows it; nothing is printed for it, the document was.
func (e Envelope) Err() error {
	if e.ExitCode == ExitOK {
		return nil
	}
	return &ExitError{Code: e.ExitCode, Verdict: e.Verdict, Reason: e.Reason}
}

// Document is a command's JSON document: a pointer to a struct that embeds
// [Envelope] and adds the command's own fields.
type Document interface {
	Finish(now time.Time, ok Verdict, err error)
	Err() error
}

// Report finishes doc from the command's error, the ok verdict when there is
// none, writes it on w and returns what the command returns to cobra: nil on
// exit 0, otherwise the [*ExitError] the process exit code follows.
func Report(w io.Writer, doc Document, ok Verdict, err error) error {
	doc.Finish(time.Now(), ok, err)
	if err := Emit(w, doc); err != nil {
		return err
	}
	return doc.Err()
}

// FlagError is the error of a flag cobra could not parse, as a command's
// reason: the parser's message and where the flags are listed. The command
// reports it with its document, exit 7, like any other wrong call.
func FlagError(command string, err error) error {
	return fmt.Errorf("%w; devctl %s --help lists the flags", err, command)
}

// Emit writes document as one indented JSON document followed by a newline.
func Emit(w io.Writer, document any) error {
	b, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the document: %w", err)
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// ExitCoder is an error that knows its place in the exit-code table.
type ExitCoder interface {
	error
	ExitCode() int
	ExitVerdict() Verdict
}

// ExitError is an outcome with its exit code: what a command returns after
// its document is written, and what any error of the table can be expressed
// as.
type ExitError struct {
	Code    int
	Verdict Verdict
	Reason  string
}

// NewExitError returns an ExitError with a formatted reason.
func NewExitError(code int, verdict Verdict, format string, args ...any) *ExitError {
	return &ExitError{Code: code, Verdict: verdict, Reason: fmt.Sprintf(format, args...)}
}

func (e *ExitError) Error() string { return e.Reason }

// ExitCode implements [ExitCoder].
func (e *ExitError) ExitCode() int { return e.Code }

// ExitVerdict implements [ExitCoder].
func (e *ExitError) ExitVerdict() Verdict { return e.Verdict }

// Exit maps err to the process exit code: 0 for nil, the code of an
// [ExitCoder] anywhere in the chain, [ExitUsage] for any other error.
func Exit(err error) int {
	if err == nil {
		return ExitOK
	}
	code, _ := Outcome(err)
	return code
}

// Outcome is err's place in the exit-code table: the code and verdict of an
// [ExitCoder] anywhere in the chain, [ExitUsage] for any other error. err
// must not be nil.
func Outcome(err error) (int, Verdict) {
	var coder ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode(), coder.ExitVerdict()
	}
	return ExitUsage, VerdictUsage
}
