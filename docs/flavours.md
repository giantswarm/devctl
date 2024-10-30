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

## `k8sapi`

A repository that provides a Kubernetes API (usually one or several custom resource definitions).

## `generic`

A repository that does not fit any of the more specific flavours above.

## `fleet`

A repository to be used with GitOps containing kubernetes clusters.
