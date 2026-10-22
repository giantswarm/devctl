# devctl

devctl serve on a Giant Swarm management cluster — devctl's agent-facing commands as MCP tools behind muster, acting on GitHub as the calling person through the installation's agents App

**Homepage:** <https://github.com/giantswarm/devctl>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Giant Swarm | <team-bumblebee@giantswarm.io> |  |

## Source Code

* <https://github.com/giantswarm/devctl>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| nameOverride | string | `""` | Override the chart name. |
| fullnameOverride | string | `""` | Override the full release name. |
| replicaCount | int | `1` | Replicas. A merge lives in the memory of the replica whose call started it (pr_merge_status reads it there), so the server runs one. |
| revisionHistoryLimit | int | `3` |  |
| image.registry | string | `"gsoci.azurecr.io"` |  |
| image.repository | string | `"giantswarm/devctl"` |  |
| image.tag | string | `""` | Empty is the chart's appVersion: the devctl release the chart ships with, so the server and the binary share a version. |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| imagePullSecrets | list | `[]` |  |
| progressInterval | string | `"10s"` | How often a wait sends notifications/progress, a Go duration: below muster.mcpServer.timeout, which each notification restarts. |
| noProgressTimeout | string | `"20s"` | How long a wait called without a progressToken runs before it answers, a Go duration: below muster.mcpServer.timeout, as nothing keeps the call alive. |
| github.grantApp | string | `""` | The provider App muster keeps whose grant carries the person's GitHub token (muster's providerApps entry, the workspace-manager's provider instance name): muster attaches it in X-Muster-Grant-<grantApp> and the server acts on GitHub with it. Required with muster.mcpServer.enabled. |
| github.app | string | `""` | The GitHub App behind grantApp (`<installation>-agents`), as an auth_required or not_applicable answer names it; empty is grantApp. |
| metrics.enabled | bool | `true` | Serve the call metrics (devctl_mcp_tool_calls_total and devctl_mcp_tool_call_duration_seconds, by tool, the caller's GitHub login and user id, repository and verdict) at /metrics on port. |
| metrics.port | int | `9090` |  |
| metrics.serviceMonitor.enabled | bool | `false` | Render a ServiceMonitor for the metrics port. |
| metrics.serviceMonitor.interval | string | `""` | Scrape interval; empty uses the Prometheus Operator default. |
| metrics.serviceMonitor.labels | object | `{}` | Labels on the ServiceMonitor. The Giant Swarm observability platform routes a scrape to a Mimir tenant by observability.giantswarm.io/tenant. |
| muster.mcpServer.enabled | bool | `false` | Register the server with muster: an MCPServer in the release namespace, which muster watches. Needs github.grantApp: one values change registers the server on an installation. |
| muster.mcpServer.name | string | `"devctl"` | The MCPServer's name. |
| muster.mcpServer.displayName | string | `"devctl"` |  |
| muster.mcpServer.description | string | `"devctl's waits and merges as the calling person, through the installation's agents App"` |  |
| muster.mcpServer.autoStart | bool | `true` |  |
| muster.mcpServer.timeout | int | `30` | How long muster waits for an answer or the next progress notification, in seconds (1 to 300). |
| muster.mcpServer.progressMaxDuration | int | `3600` | How long progress keeps one call going at most, in seconds (at most 3600): above the server's longest wait, 55 minutes. |
| muster.mcpServer.requiredAudiences | list | `[]` | Extra audiences the forwarded ID token carries; muster requests them at sign-in. The server reads no ID token: muster forwards one only because it attaches the grant to a connection made as the person. |
| muster.mcpServer.labels | object | `{}` | Extra labels on the MCPServer. |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `""` |  |
| service.port | int | `8080` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `65532` |  |
| podSecurityContext.runAsGroup | int | `65532` |  |
| podSecurityContext.fsGroup | int | `65532` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.readOnlyRootFilesystem | bool | `true` |  |
| securityContext.runAsNonRoot | bool | `true` |  |
| securityContext.runAsUser | int | `65532` |  |
| securityContext.runAsGroup | int | `65532` |  |
| securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| securityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| resources.requests.cpu | string | `"50m"` |  |
| resources.requests.memory | string | `"64Mi"` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| extraArgs | list | `[]` | Extra arguments to devctl serve. |
| extraEnv | list | `[]` | Extra environment variables (DEVCTL_GITHUB_API_URL and the other endpoints of devctl's agent-facing commands). |
| nodeSelector | object | `{}` |  |
| tolerations | list | `[]` |  |
| affinity | object | `{}` |  |
