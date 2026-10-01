package changelog

// kubernetesComponentName is the component whose changelog is read from the
// upstream Kubernetes release notes rather than from a chart CHANGELOG.
const kubernetesComponentName = "kubernetes"

// clusterChartUpdateFilter drops the automated cluster-chart bump entry from a
// provider chart's changelog, where it only repeats the cluster entry.
const clusterChartUpdateFilter = "Chart: Update `cluster`"
