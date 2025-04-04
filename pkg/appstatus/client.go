package appstatus

import (
	"context"
	"io"
	"os/exec"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
)

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
}

type Client struct {
	logger *logrus.Logger
	stderr io.Writer
}

func New(config Config) (*Client, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Stderr must not be empty", config)
	}

	c := &Client{
		logger: config.Logger,
		stderr: config.Stderr,
	}

	return c, nil
}

func (c *Client) WaitForAppDeployment(ctx context.Context, appName, orgNamespace, managementCluster string, timeout time.Duration) error {
	c.logger.Infof("Waiting for app %s to be deployed in namespace %s", appName, orgNamespace)

	// Login to management cluster
	loginCmd := exec.Command("tsh", "kube", "login", managementCluster)
	err := loginCmd.Run()
	if err != nil {
		return microerror.Mask(err)
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		// Get app status using kubectl
		kubectlCmd := exec.Command("kubectl", "get", "app", appName,
			"-n", orgNamespace,
			"-o", "jsonpath={.status.release.status}")
		kubectlCmd.Stderr = c.stderr

		output, err := kubectlCmd.Output()
		status := string(output)
		if err == nil {
			if status == "deployed" {
				c.logger.Infof("App %s successfully deployed", appName)
				return microerror.Mask(err)
			}
		}

		select {
		case <-ticker.C:
			c.logger.Infof("App %s is not deployed yet, current status: %s", appName, status)
		case <-ctx.Done():
			return microerror.Maskf(invalidConfigError, "App %s was not deployed within %v", appName, timeout)
		}
	}
}
