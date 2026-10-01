package appstatus

import (
	"context"
	"io"
	"os/exec"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/internal/validate"
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
	// The three names become arguments of tsh and kubectl, so this package
	// constrains them itself rather than trust every caller to do it.
	if err := validate.Name("app name", appName); err != nil {
		return microerror.Maskf(invalidConfigError, "%s", err)
	}
	if err := validate.Name("organization namespace", orgNamespace); err != nil {
		return microerror.Maskf(invalidConfigError, "%s", err)
	}
	if err := validate.Name("management cluster", managementCluster); err != nil {
		return microerror.Maskf(invalidConfigError, "%s", err)
	}

	c.logger.Infof("Waiting for app %s to be deployed in namespace %s", appName, orgNamespace)

	// Login to management cluster
	loginCmd := exec.Command("tsh", "kube", "login", managementCluster) // #nosec G204 -- fixed binary; managementCluster is checked above and passed as its own argument element
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
		kubectlCmd := exec.Command("kubectl", "get", "app", appName, // #nosec G204 -- fixed binary and subcommand; appName and orgNamespace are checked above and passed as their own argument elements
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
