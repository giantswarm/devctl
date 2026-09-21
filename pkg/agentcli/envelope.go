// Package agentcli is what devctl's agent-facing commands share: the JSON
// envelope every one of them prints as its only stdout output, the exit-code
// table, the clock that DEVCTL_TIME_SCALE speeds up for tests, the endpoint
// configuration read from the environment and the --progress writer.
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
	VerdictGreen           Verdict = "green"
	VerdictRed             Verdict = "red"
	VerdictTimeout         Verdict = "timeout"
	VerdictNotApplicable   Verdict = "not_applicable"
	VerdictRequiredMissing Verdict = "required_missing"
	VerdictRefused         Verdict = "refused"
	VerdictAvailable       Verdict = "available"
	VerdictCIFailed        Verdict = "ci_failed"
	VerdictAuthRequired    Verdict = "auth_required"
	VerdictUsage           Verdict = "usage"
)

// The exit codes of every agent-facing command. 6 is unused.
const (
	// ExitOK: the wait ended green, the merge happened, the release is available.
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
	// ExitUsage: wrong usage or a tooling failure.
	ExitUsage = 7
	// ExitAuthRequired: no usable token; the reason names `devctl auth login`.
	ExitAuthRequired = 8
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
	e.ExitCode, e.Verdict = outcome(err)
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
	code, _ := outcome(err)
	return code
}

func outcome(err error) (int, Verdict) {
	var coder ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode(), coder.ExitVerdict()
	}
	return ExitUsage, VerdictUsage
}
