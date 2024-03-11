package ciwebhooks

import (
	"github.com/spf13/cobra"
)

type flag struct {
	DryRun            bool
	GithubTokenEnvVar string

	WebhookURL          string
	WebhookSharedSecret string
}

func (f *flag) Init(cmd *cobra.Command) {
	// Persistent flags are also available to subcommands.
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for Github token.")

	// Standard flags
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Dry-run or ready-only mode. Show what is being made but do not apply any change.")

	// Webhook
	cmd.Flags().StringVar(&f.WebhookURL, "webhook-url", "https://github-pr-webhook.ci.giantswarm.io", "The URL to send the webhook to")
	cmd.Flags().StringVar(&f.WebhookSharedSecret, "webhook-secret", "", "The shared secret for the webhook configuration (required)")
	_ = cmd.MarkFlagRequired("webhook-secret")
}

func (f *flag) Validate() error {
	return nil
}
