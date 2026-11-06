package deploy

import (
	"context"
	"time"

	"github.com/giantswarm/microerror"
)

type mergeWaiter interface {
	WaitForPRMerge(context.Context, string, string, int, time.Duration) error
}

type deploymentWaiter interface {
	WaitForAppDeployment(context.Context, string, string, string, time.Duration) error
}

func (r *runner) waitForDeployment(ctx context.Context, owner, repo string, prNumber int, merge mergeWaiter, deployment deploymentWaiter) error {
	if err := merge.WaitForPRMerge(ctx, owner, repo, prNumber, r.Flag.Timeout); err != nil {
		return microerror.Mask(err)
	}

	orgNamespace := "org-" + r.Flag.Organization
	appName := r.Flag.WorkloadCluster + "-" + r.Flag.AppName
	if err := deployment.WaitForAppDeployment(ctx, appName, orgNamespace, r.Flag.ManagementCluster, r.Flag.Timeout); err != nil {
		return microerror.Mask(err)
	}

	return nil
}
