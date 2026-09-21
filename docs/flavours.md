# Understanding flavours

`devctl` understands different types of GitHub repositories, to make the right modifications for each type. App repos have different needs than Go libraries, for example.

Flavours are not mutually exclusive. Some repos must be configured with multiple flavours. For example, an operator may carry the flavours `app` and `k8sapi`. Note: this configuration is usually persisted in our [repository configuration](https://github.com/giantswarm/github/tree/main/repositories).

The following flavours are understood:

## `app`

An app, by the definition of the Giant Swarm app platform. Each app repository contains at lest one Helm chart.

## `cli`

A command line interface (CLI) component which is typically executed on-demand either by a user or within an automation system. CLIs are typically released with downloadable and executable binaries.

## `cluster-app`

A specific type of app repository which provides a values schema that aims to fulfill the requirements of the [RFC #55](https://github.com/giantswarm/rfc/pull/55).

## `customer`

A repository used to track mostly issues and provide project boards, shared with a customer.

Its generated setup is the GitHub workflows (`gen workflows`, including the customer board automation) plus `renovate.json5` (`gen renovate --language generic`: the giantswarm base preset, nothing language- or CI-specific). There is no CircleCI for customer repositories, so align-files runs `gen renovate` for this flavour without `--circleci-generated`, and a newly registered customer repository has its Renovate config before Renovate's first run instead of receiving the onboarding PR.

## `fork`

A fork line: a repository that carries an upstream release plus the carried patches on a branch named after the organisation, which its entry declares as `defaultBranch`. The tree is upstream's, so nothing is generated for it: every `devctl gen` leaves it as it is, and the repository set-up reconciler skips the scaffold and CODEOWNERS steps on it and runs every other step as declared, branch protection on the declared branch included. Declared alone, with the repository's language.

## `k8sapi`

A repository that provides a Kubernetes API (usually one or several custom resource definitions).

## `generic`

A repository that does not fit any of the more specific flavours above.

## `fleet`

A repository to be used with GitOps containing kubernetes clusters.
