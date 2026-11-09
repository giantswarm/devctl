package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestExecuteReportsEveryJoinedFailure(t *testing.T) {
	for _, debugLog := range []bool{false, true} {
		t.Run(fmt.Sprint(debugLog), func(t *testing.T) {
			root := &cobra.Command{Use: "devctl", SilenceErrors: true, SilenceUsage: true,
				RunE: func(*cobra.Command, []string) error {
					first := microerror.Maskf(&microerror.Error{Kind: "invalidConfigError"}, "bad reservation date for app-one")
					second := microerror.Maskf(&microerror.Error{Kind: "invalidConfigError"}, "bad reservation date for app-two")
					return microerror.Mask(fmt.Errorf("sweeping reservations: %w", errors.Join(
						fmt.Errorf("cluster graveler: %w", first),
						fmt.Errorf("cluster zz-last: %w", errors.Join(second, errors.New("output destination failed"))),
					)))
				},
			}
			root.PersistentFlags().String(flagLogLevel, "info", "log level")
			if debugLog {
				root.SetArgs([]string{"--log-level", "debug"})
			} else {
				root.SetArgs(nil)
			}
			var stderr bytes.Buffer
			if code := Execute(root, &stderr); code != exitError {
				t.Errorf("exit %d, want %d", code, exitError)
			}
			for _, want := range []string{"sweeping reservations", "cluster graveler", "app-one", "cluster zz-last", "app-two", "output destination failed"} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr omitted %q: %s", want, stderr.String())
				}
			}
			hasTrace := strings.Contains(stderr.String(), "execute_joined_test.go:")
			if hasTrace != debugLog {
				t.Errorf("debug=%v, trace=%v: %s", debugLog, hasTrace, stderr.String())
			}
		})
	}
}

func TestExecuteJoinedAgentErrorStaysSilent(t *testing.T) {
	root := &cobra.Command{Use: "devctl", SilenceErrors: true, SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error {
			return errors.Join(errors.New("already reported"), agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "bad arguments"))
		},
	}
	root.SetArgs(nil)
	var stderr bytes.Buffer
	if code := Execute(root, &stderr); code != agentcli.ExitUsage {
		t.Errorf("exit %d, want %d", code, agentcli.ExitUsage)
	}
	if stderr.Len() != 0 {
		t.Errorf("agent error printed a second diagnostic: %q", stderr.String())
	}
}
