package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestExecuteUnknownSubcommandOfNonRunnableGroup(t *testing.T) {
	for _, args := range [][]string{{"ci", "jbos"}, {"rollout", "wiat"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := newTestRoot(t, &stdout)
			root.SetArgs(args)
			if code := Execute(root, &stderr); code != exitError {
				t.Errorf("exit %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout %q, want none", stdout.String())
			}
			want := "Error: Unknown command \"" + args[1] + "\" for \"devctl " + args[0] + "\""
			if !strings.HasPrefix(stderr.String(), want) || !strings.Contains(stderr.String(), "did you mean") || !strings.Contains(stderr.String(), "Run 'devctl "+args[0]+" --help'") {
				t.Errorf("stderr %q, want the unknown-command error, suggestion and group help pointer", stderr.String())
			}
		})
	}
}

// Turning these groups into runnable commands must keep their help available
// without the version gate, a keychain, or network access.
func TestExecuteNonRunnableGroupHelpOffline(t *testing.T) {
	hits := offlineContainer(t)
	for _, args := range [][]string{{"ci"}, {"rollout"}, {"ci", "--help"}, {"rollout", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out := execute(t, args...)
			if code != 0 || !strings.Contains(out, "Available Commands:") {
				t.Errorf("exit %d, output %q; want help and exit 0", code, out)
			}
		})
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("GitHub was asked %d times, want 0", n)
	}
}
