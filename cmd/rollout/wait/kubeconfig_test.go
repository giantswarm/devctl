package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

func TestRunKubeconfigDiagnostics(t *testing.T) {
	for _, source := range []string{"KUBECONFIG", "DEVCTL_KUBECONFIG", "flag"} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			machine, private := filepath.Join(dir, "machine"), filepath.Join(dir, "private config")
			const config = "apiVersion: v1\nkind: Config\ncontexts: []\n"
			for _, path := range []string{machine, private} {
				if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Keep client-go's legacy migration check inside the fixture too.
			previousHome := clientcmd.RecommendedHomeFile
			clientcmd.RecommendedHomeFile = machine
			t.Cleanup(func() { clientcmd.RecommendedHomeFile = previousHome })
			t.Setenv("KUBECONFIG", machine)
			t.Setenv("DEVCTL_KUBECONFIG", "")
			args := []string{"myinstallation", "giantswarm/devctl", "--pr", "1234"}
			searched, excluded := machine, private
			if source != "KUBECONFIG" {
				searched, excluded = private, machine
				t.Setenv("DEVCTL_KUBECONFIG", private)
			}
			if source == "flag" {
				args = append(args, "--kubeconfig", private)
				t.Setenv("DEVCTL_KUBECONFIG", filepath.Join(dir, "unused-env"))
			}

			var stdout, stderr bytes.Buffer
			f := &flag{}
			r := &runner{
				flag: f, stdout: &stdout, stderr: &stderr,
				gate:        func(bool) error { return nil },
				openCluster: laptop.OpenCluster,
				openRelease: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
					t.Fatal("a missing context must end the command before opening release sources")
					return nil, nil
				},
			}
			cmd := &cobra.Command{Use: "wait", RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
			f.Init(cmd)
			cmd.SetArgs(args)
			err := cmd.Execute()
			if agentcli.Exit(err) != agentcli.ExitUsage {
				t.Fatalf("want exit 7, got %d (%v)", agentcli.Exit(err), err)
			}
			var doc rolloutwait.Document
			decoder := json.NewDecoder(&stdout)
			if err := decoder.Decode(&doc); err != nil {
				t.Fatal(err)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatalf("want exactly one document, got %v", err)
			}
			if doc.ExitCode != agentcli.ExitUsage || doc.Verdict != agentcli.VerdictUsage || doc.Context != "teleport.giantswarm.io-myinstallation" {
				t.Errorf("unexpected envelope: %+v", doc.Envelope)
			}
			for _, want := range []string{doc.Context, searched, "kubeconfig files searched:", "--kubeconfig <file>", "DEVCTL_KUBECONFIG", "KUBECONFIG"} {
				if !strings.Contains(doc.Reason, want) {
					t.Errorf("reason %q does not contain %q", doc.Reason, want)
				}
			}
			if strings.Contains(doc.Reason, excluded) || strings.Contains(doc.Reason, "unused-env") {
				t.Errorf("reason names a file that was not searched: %s", doc.Reason)
			}
			if stderr.Len() != 0 {
				t.Errorf("unexpected stderr: %s", &stderr)
			}
		})
	}
}
