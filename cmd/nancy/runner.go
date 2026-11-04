package nancy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const keychainUser = "ossindex"

type runner struct {
	getRecord func(string) (authstore.Record, error)
	command   func(context.Context, string, ...string) *exec.Cmd
	stderr    io.Writer
	stdout    io.Writer
}

func (r runner) run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}
	record, err := r.getRecord(keychainUser)
	if err != nil {
		cause := "cannot read devctl/ossindex from the OS keychain"
		if errors.Is(err, authstore.ErrNotFound) {
			cause = "no devctl/ossindex record in the OS keychain"
		}
		// A malformed record's decoding error can contain its contents.
		return credentialsRequired(cause)
	}
	if strings.TrimSpace(record.Login) == "" || strings.TrimSpace(record.Token) == "" {
		return credentialsRequired("devctl/ossindex needs both login and token")
	}
	if !record.ExpiresAt.IsZero() && !time.Now().Before(record.ExpiresAt) {
		return credentialsRequired("the devctl/ossindex token expired")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return microerror.Mask(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var outputMu sync.Mutex
	stdout := synchronizedWriter{mu: &outputMu, writer: r.stdout}
	stderr := synchronizedWriter{mu: &outputMu, writer: r.stderr}

	// The listing gets no scanner credentials. Inherited Guide credentials must
	// not take precedence over the keychain's OSS Index identity in nancy.
	env := scanEnvironment(os.Environ())
	deps := r.command(ctx, "go", "list", "-json", "-deps", "./...")
	deps.Env = append(env, "CGO_ENABLED=0")
	deps.Stderr = stderr
	input, err := deps.StdoutPipe()
	if err != nil {
		return microerror.Mask(err)
	}
	defer input.Close()

	// An explicitly empty Guide flag overrides any Guide token in nancy's
	// own config file. No credential is passed on the command line.
	scan := r.command(ctx, "nancy", "sleuth", "--guide-token=", "--skip-update-check",
		"--db-cache-path", filepath.Join(home, ".cache", "nancy"), "--quiet",
		"--exclude-vulnerability-file", "./.nancy-ignore",
		"--additional-exclude-vulnerability-files", "./.nancy-ignore.generated")
	scan.Env = append(scanEnvironment(os.Environ()), "CGO_ENABLED=0",
		"OSSI_USERNAME="+record.Login, "OSSI_TOKEN="+record.Token)
	scan.Stdin = input
	scan.Stdout = stdout
	scan.Stderr = stderr

	if err := deps.Start(); err != nil {
		return microerror.Mask(fmt.Errorf("starting go list: %w", err))
	}
	scanErr := scan.Run()
	if scanErr != nil {
		cancel()
	}
	// Close our reader before waiting: a scanner that exits before consuming
	// the list must not leave go blocked writing a full pipe.
	_ = input.Close()
	listErr := deps.Wait()
	if scanErr != nil {
		return processError("nancy", scanErr)
	}
	return processError("go list", listErr)
}

// Both child processes can write diagnostics concurrently, and the caller
// may also have given stdout and stderr the same buffer.
type synchronizedWriter struct {
	mu     *sync.Mutex
	writer io.Writer
}

func (w synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

func scanEnvironment(env []string) []string {
	clean := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "OSSI_USERNAME", "OSSI_TOKEN", "GUIDE_TOKEN", "CGO_ENABLED":
			continue
		}
		clean = append(clean, entry)
	}
	return clean
}

func credentialsRequired(cause string) error {
	return &authstore.AuthRequiredError{
		Identity: "OSS Index",
		Cause:    cause + "; see docs/nancy.md",
		Hint:     "devctl nancy --help",
	}
}

func processError(name string, err error) error {
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		// The subprocess has already written its diagnostic.
		return &agentcli.ExitError{Code: exit.ExitCode()}
	}
	return microerror.Mask(fmt.Errorf("running %s: %w", name, err))
}
