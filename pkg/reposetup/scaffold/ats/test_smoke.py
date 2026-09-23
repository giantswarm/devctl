"""ATS smoke for the {APP-NAME} chart.

app-test-suite (ATS >= 1.0) installs the packaged chart on the job's kind
cluster with `helm upgrade --install --wait` (the namespace from
.ats/main.yaml) and runs this file with `pytest -m smoke`: the generated
execute-chart-tests job of every branch build. The scaffold's chart deploys
nothing yet, so the smoke is the cluster-level check alone: the kind cluster
ATS runs against is reachable. Extend it with the chart's own checks as the
chart grows -- its Deployment ready
(pytest_helm_charts.k8s.deployment.wait_for_deployments_to_run), its CRDs
established -- and mark the checks that must hold across a chart upgrade with
@pytest.mark.upgrade once .ats/main.yaml runs the upgrade scenario. The
dependencies are the generated tests/ats/pyproject.toml, owned by
giantswarm/devctl; this file and .ats/main.yaml are the repository's own.
"""

import pykube
import pytest
from pytest_helm_charts.clusters import Cluster


@pytest.mark.smoke
def test_api_working(kube_cluster: Cluster) -> None:
    """The kind cluster ATS runs against is reachable."""
    assert kube_cluster.kube_client is not None
    assert len(pykube.Node.objects(kube_cluster.kube_client)) >= 1
