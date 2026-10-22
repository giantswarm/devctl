package laptop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ErrNoTsh is what Teleport answers when tsh is not installed: the kube
// context stays missing, nothing logs in.
var ErrNoTsh = errors.New("tsh is not installed")

// Teleport logs in to an installation's management cluster, what
// OpenCluster falls back to for the Teleport context the kubeconfig lacks.
type Teleport interface {
	// LoggedIn reports whether tsh holds a profile that is valid now, and
	// when not, why. ErrNoTsh when tsh is not installed.
	LoggedIn(ctx context.Context) (bool, string, error)
	// KubeLogin writes the installation's context, named kubeContext, into
	// the kubeconfig file, and nowhere else.
	KubeLogin(ctx context.Context, installation, kubeContext, kubeconfig string) error
}

// Tsh is Teleport through the tsh binary.
type Tsh struct {
	// Binary is the tsh to run; empty is tsh from PATH.
	Binary string
	// Now is the clock a profile's expiry is compared with; nil is
	// time.Now.
	Now func() time.Time
}

// LoginTimeout bounds every tsh call: a profile checked valid does not
// prompt, so a call that does not return is a hung proxy.
const LoginTimeout = time.Minute

func (t Tsh) binary() (string, error) {
	name := t.Binary
	if name == "" {
		name = "tsh"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", ErrNoTsh
	}
	return path, nil
}

// LoggedIn reads tsh status --format=json: no active profile, or one whose
// certificate has expired, is not logged in. tsh kube login would start an
// interactive login for either, which the check rules out.
func (t Tsh) LoggedIn(ctx context.Context) (bool, string, error) {
	path, err := t.binary()
	if err != nil {
		return false, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, LoginTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "status", "--format=json") // #nosec G204 -- tsh from exec.LookPath, fixed arguments
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return false, firstLine(stderr.String(), err.Error()), nil
	}
	var status struct {
		Active *struct {
			ProfileURL string    `json:"profile_url"`
			ValidUntil time.Time `json:"valid_until"`
		} `json:"active"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return false, "", fmt.Errorf("reading tsh status: %w", err)
	}
	if status.Active == nil {
		return false, "no active tsh profile", nil
	}
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	if !status.Active.ValidUntil.After(now()) {
		return false, fmt.Sprintf("the tsh profile %s expired at %s", status.Active.ProfileURL, status.Active.ValidUntil.Format(time.RFC3339)), nil
	}
	return true, "", nil
}

// KubeLogin runs tsh kube login with KUBECONFIG set to kubeconfig for this
// one command, the context named kubeContext.
func (t Tsh) KubeLogin(ctx context.Context, installation, kubeContext, kubeconfig string) error {
	path, err := t.binary()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, LoginTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "kube", "login", installation, "--set-context-name="+kubeContext) // #nosec G204 -- tsh from exec.LookPath; the installation is checked by the caller and passed as its own argument element
	cmd.Env = append(withoutEnv(os.Environ(), "KUBECONFIG"), "KUBECONFIG="+kubeconfig)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tsh kube login %s: %s", installation, firstLine(string(out), err.Error()))
	}
	return nil
}

// withoutEnv is env without the variable key.
func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return out
}

// ansi matches the terminal colour codes tsh puts around ERROR.
var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// firstLine is the first non-empty line of s without colour codes, fallback
// when there is none.
func firstLine(s, fallback string) string {
	for line := range strings.Lines(ansi.ReplaceAllString(s, "")) {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return fallback
}
