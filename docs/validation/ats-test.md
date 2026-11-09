# Generated ATS target: real-repository validation

Acceptance proof for [the local ATS target](https://github.com/giantswarm/devctl/issues/2734), exercised through [the implementation PR](https://github.com/giantswarm/devctl/pull/3110).

## Inputs

- Generator: devctl `bd283d1f71f7c31ce7ca44457e7148b1256bd402`, built from the reviewed PR head.
- Consumer: [giantswarm/klaus](https://github.com/giantswarm/klaus/tree/bc4100bebf1ec2420975501226bccedc34a776ff) at release `v1.0.16` (`bc4100bebf1ec2420975501226bccedc34a776ff`) in a detached worktree.
- Suite: the repository's existing `.ats/main.yaml`, `tests/ats/test_basic_cluster.py`, `tests/ats/pyproject.toml`, `tests/ats/uv.lock` and `tests/test-values.yaml`, unchanged. Smoke is enabled; functional and upgrade are skipped by the repository.
- ATS: `gsoci.azurecr.io/giantswarm/app-test-suite:1.0.2`, selected with the supported version override. The matching-release run below proves the real target and suite together.
- Node: the generated default, `gsoci.azurecr.io/giantswarm/kind-node:v1.37.0` (`sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`).
- Application: the publicly pullable `gsoci.azurecr.io/giantswarm/klaus:1.0.16`; no local application image replacement.
- Tools: kind v0.33.0, Docker 29.9.3, Helm v4.3.0, Linux amd64.

## Reproduction

With `devctl-under-test` built from the generator revision above, run these commands in the consumer worktree:

```sh
devctl-under-test gen makefile --flavour app --language go
mkdir -p build
helm package helm/klaus --version 1.0.16 --app-version 1.0.16 --destination build
make ats-test ATS_VERSION=1.0.2 ATS_CHART_FILE=build/klaus-1.0.16.tgz
```

The chart is packaged from the consumer revision above, with the released application version selected explicitly. The generated target is used as emitted; kind and Docker are the installed executables, with no recording fixtures. The run uses host networking and its own temporary kubeconfig. The calling shell's cluster context is not selected.

Generated `Makefile.gen.app.mk` SHA-256:
`4319d1980efd8db0bc83adafcb8bf06d3cbd782f4f1ba8a4c2d5e69a583fa6ca`.

Built `build/klaus-1.0.16.tgz` SHA-256:
`81dacaefc382e9ec060b6eba968d61246559e4bcc74eeffea4eab44c78b31dd9`.

## Outcome

Passed on 2026-11-06 at 16:08 UTC. The temporary kind cluster `ats-gucs96` became Ready; ATS applied its cluster CRDs, installed the built chart as release `klaus`, and ran the repository's two smoke tests:

```text
test_basic_cluster.py::test_api_working PASSED
test_basic_cluster.py::test_pods_available PASSED
2 passed in 0.03s
release "klaus" uninstalled
Deleting cluster "ats-gucs96" ...
Deleted nodes: ["ats-gucs96-control-plane"]
Clusters after:
No kind clusters found.
PROOF PASSED: make ats-test exit 0; cluster and temporary directory cleaned up
```

The durable run completed with `Result=success`, `ExecMainStatus=0`. No kind clusters existed before or after the run; no `.ats-run.*` directory remained. Both smoke tests ran; neither was replaced, skipped or marked successful by a fixture.
