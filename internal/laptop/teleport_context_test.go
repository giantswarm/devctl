package laptop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestTshRunningDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tsh is a shell script")
	}
	// Stop the fixture until CommandContext kills it; no proxy or login is used.
	binary := writeFile(t, "tsh", "#!/bin/sh\nkill -STOP $$\n")
	if err := os.Chmod(binary, 0o700); err != nil { // #nosec G302 -- the fake must be executable
		t.Fatal(err)
	}
	tsh := Tsh{Binary: binary}
	for _, phase := range []string{"status", "login"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			var err error
			if phase == "status" {
				_, _, err = tsh.LoggedIn(ctx)
			} else {
				err = tsh.KubeLogin(ctx, "myinstallation", teleportContext, filepath.Join(t.TempDir(), "config"))
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("deadline stopping tsh %s must retain its cause, got %v", phase, err)
			}
		})
	}
}

func TestTshPreservesContextErrors(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			tsh := installFakeTsh(t, time.Now())
			var ctx context.Context
			if cause == context.Canceled {
				canceled, cancel := context.WithCancel(context.Background())
				cancel()
				ctx = canceled
			} else {
				expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				ctx = expired
			}

			loggedIn, _, err := tsh.LoggedIn(ctx)
			if loggedIn || !errors.Is(err, cause) {
				t.Errorf("interrupted status must retain %v, got loggedIn=%t, err=%v", cause, loggedIn, err)
			}
			private := filepath.Join(t.TempDir(), "config")
			if err := tsh.KubeLogin(ctx, "myinstallation", teleportContext, private); !errors.Is(err, cause) {
				t.Errorf("interrupted kube login must retain %v, got %v", cause, err)
			}
			if _, err := os.Stat(private); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("an interrupted login must not write a kubeconfig: %v", err)
			}
		})
	}
}

func TestOpenClusterPreservesTeleportContextErrors(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, phase := range []string{"status", "login"} {
			t.Run(cause.Error()+"/"+phase, func(t *testing.T) {
				machineKubeconfig(t)
				teleport := &fakeTeleport{loggedIn: true}
				if phase == "status" {
					teleport.err = cause
				} else {
					teleport.loginErr = cause
				}
				client, err := OpenCluster(context.Background(), ClusterConfig{
					Installation: "myinstallation",
					Context:      teleportContext,
					Teleport:     teleport,
				})
				if client != nil || !errors.Is(err, cause) {
					t.Fatalf("interrupted %s must retain %v, got client=%T, err=%v", phase, cause, client, err)
				}
				var outcome agentcli.ExitCoder
				if errors.As(err, &outcome) {
					t.Errorf("interrupted %s must not become an authentication/usage outcome: %v", phase, err)
				}
				if phase == "login" {
					if _, err := os.Stat(filepath.Dir(teleport.kubeconfig)); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("private directory must be removed after interrupted login: %v", err)
					}
				}
			})
		}
	}
}
