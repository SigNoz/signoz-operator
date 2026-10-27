# Operator tests

The e2e suite creates a [Kind](https://kind.sigs.k8s.io/) cluster, installs SigNoz with `foundryctl`, builds this checkout's operator image, loads it into Kind and applies the operator overlay. Tests then apply custom resources and assert both what reached SigNoz over its HTTP API and the conditions the operator reported.

## Requirements

- Docker
- Kind
- kubectl
- foundryctl
- Python 3.13 and uv

## Run

From the repository root:

```sh
make test-e2e
```

This removes the cluster and Foundry's generated files afterwards. To keep the environment between runs:

```sh
make test-e2e-reuse
kubectl --kubeconfig tests/tmp/kubeconfig get pods --all-namespaces
make cleanup-test-e2e
```

`make setup-e2e-env` brings the environment up without running the tests. Narrow a run with `E2E_TARGET`, relative to `tests/`:

```sh
make test-e2e-reuse E2E_TARGET=e2e/tests/dashboard/01_dashboard.py
make test-e2e-reuse E2E_TARGET=e2e/tests/dashboard/01_dashboard.py::test_a_change_made_in_signoz_is_reverted
```

The retained environment is recorded in pytest's cache, so `--reuse` only connects to a cluster this suite created, and a non-reuse run refuses to take over an existing cluster of the same name.

In CI, `e2eci` runs the suite on a pull request labelled `safe-to-e2e`. It installs the latest Foundry and pins kind, which decides the Kubernetes version.

## Reaching SigNoz

The operator talks to SigNoz in-cluster at `http://signoz-signoz.signoz.svc.cluster.local:8080`. The test process reaches the same API from the host through the NodePort in `e2e/signoz/service.yaml`, which `kind.yaml` maps to `127.0.0.1:30080`; `SIGNOZ_URL` in `fixtures/signoz.py` carries the same port.

Readiness is `/api/v2/healthz`, which needs no credential. SigNoz serves its web app on any unrouted path, so the check reads `data.healthy` rather than the status code.

`casting.yaml` sets `SIGNOZ_USER_ROOT_*`, so SigNoz provisions its org and root admin at boot and keeps them across restarts and `--reuse` runs. The suite logs in as that user, grants a service account `signoz-admin`, and issues it an API key, the same credential a real `ProviderConfig` carries.

## Foundry overrides

- `SIGNOZ_USER_ROOT_ENABLED` and `SIGNOZ_STATSREPORTER_ENABLED` are `spec.patches`. Foundry renders `spec.signoz.spec.env` values unquoted, so `"true"` reaches the API server as a boolean and the StatefulSet is rejected.
- `spec.telemetrystore` deletes `query_log.partition_by`. The clickhouse-operator config Foundry ships already defines an engine for that table, and ClickHouse refuses both together and never finishes loading metadata.

## Namespaces

The operator watches every namespace, so tests run in `signoz-operator-e2e` and stay apart by a name unique to each test. The manager runs in `signoz-operator-system`, where `config/default` installs it.

## Layout

- `casting.yaml`: Foundry's installation definition.
- `kind.yaml`: the Kind cluster and its host port mapping.
- `fixtures/`: every fixture and shared helper, registered in `conftest.py`.
- `e2e/operator/`: overlay pointing `config/default` at the locally built image.
- `e2e/signoz/service.yaml`: the NodePort that publishes the SigNoz API.
- `e2e/bootstrap/setup.py`: brings the environment up; runs first as a smoke check.
- `e2e/tests/<suite>/NN_*.py`: the tests.
- `testdata/`: request bodies sent to SigNoz.
