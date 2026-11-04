package deploy

import (
	"context"
	"os/exec"
	"time"

	"github.com/giantswarm/microerror"
)

func (r *runner) addApp(ctx context.Context, dir string) error {
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}
	addAppArgs := []string{
		"gs", "gitops", "add", "app",
		"--app", r.Flag.AppName,
		"--version", r.Flag.AppVersion,
		"--catalog", r.Flag.AppCatalog,
		"--target-namespace", r.Flag.AppNamespace,
		"--management-cluster", r.Flag.ManagementCluster,
		"--workload-cluster", r.Flag.WorkloadCluster,
		"--organization", r.Flag.Organization,
	}
	addAppCmd := exec.CommandContext(ctx, "kubectl", addAppArgs...) // #nosec G204 -- fixed binary; every interpolated flag value is constrained by flag.Validate and passed as its own argument element
	addAppCmd.Dir = dir
	addAppCmd.Stdout = r.Stdout
	addAppCmd.Stderr = r.Stderr
	// A plugin can inherit the output pipes: bound draining them after
	// kubectl exits or is killed by cancellation.
	addAppCmd.WaitDelay = time.Second
	err := addAppCmd.Run()
	if ctx.Err() != nil {
		return microerror.Mask(ctx.Err())
	}
	return microerror.Mask(err)
}
