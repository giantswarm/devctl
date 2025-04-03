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
	loginCmd.Stderr = c.stderr
	output, err := loginCmd.Output()
	if err != nil {
		return microerror.Mask(err)
	}
	c.logger.Infof("Login output: %s", output)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	
	go func(appName, orgNamespace string) {
		// Get app status using kubectl
		kubectlCmd := exec.Command("kubectl", "get", "app", appName,
			"-n", orgNamespace,
			"-o", "jsonpath={.status.release.status}")
		kubectlCmd.Stderr = c.stderr

		output, err := kubectlCmd.Output()
		if err == nil {
			status := string(output)
			if status == "deployed" {
				c.logger.Infof("App %s successfully deployed", appName)
				cancel()
				return
			}
			c.logger.Infof("App %s is not deployed yet, current status: %s", appName, status)
		 }

		 <-ticker.C
	}(appName, orgNamespace)

	
	<-ctx.Done()
	return ctx.Err()
}
