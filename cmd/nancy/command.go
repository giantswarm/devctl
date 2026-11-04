package nancy

import (
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	r := runner{
		getRecord: authstore.KeyringStore{}.Get,
		command:   exec.CommandContext,
		stderr:    config.Stderr,
		stdout:    config.Stdout,
	}
	return &cobra.Command{
		Use:   "nancy",
		Short: "Scan local Go dependencies with OSS Index credentials from the OS keychain.",
		Long: `Run from the Go repository root with Go and nancy v2.1.x on PATH.

Scan CGO_ENABLED=0 go list -json -deps ./... with CircleCI's nancy sleuth
arguments, including .nancy-ignore and .nancy-ignore.generated. OSS Index
credentials come only from the OS keychain's devctl/ossindex record (login
and token), and reach nancy only through its environment. Unlock the keychain
and configure that record in your keychain application's secure editor; see
docs/nancy.md for its JSON format. No credential flags or credential files
are used by devctl.

Nancy's output and exit status are preserved. A failed dependency listing
also fails the command; scanner outages are failures, never a clean scan.`,
		Args:        cobra.NoArgs,
		Annotations: versiongate.Exempt(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return r.run(cmd.Context())
		},
	}, nil
}
