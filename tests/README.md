# Operator tests

The e2e suite creates a [Kind](https://kind.sigs.k8s.io/) cluster, installs SigNoz with `foundryctl`, and deploys the operator built from this checkout. Each test applies custom resources, then checks what reached SigNoz over its HTTP API and which conditions the operator reported.

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

This deletes the cluster and Foundry's generated files when it finishes. To keep the environment between runs:

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

A `--reuse` run rebuilds and restarts the operator, so it always tests the current checkout. The suite records the cluster it keeps in pytest's cache, and `--reuse` only connects to that cluster. A run without `--reuse` stops if a cluster with the same name already exists.

In CI, `e2eci` runs the suite on pull requests labelled `safe-to-e2e`. It installs the latest Foundry and a pinned kind release, which sets the Kubernetes version.

## Reaching SigNoz

The operator calls SigNoz in-cluster at `http://signoz-signoz.signoz.svc.cluster.local:8080`. The tests call the same API from the host through the NodePort in `e2e/signoz/service.yaml`, which `kind.yaml` maps to `127.0.0.1:30080`. `SIGNOZ_URL` in `fixtures/signoz.py` uses the same port, so change all three together.

The suite waits on `/api/v2/healthz`, which needs no credential. SigNoz answers any unknown path with its web app and a 200, so the check reads `data.healthy` from the body.

`casting.yaml` sets `SIGNOZ_USER_ROOT_*`, so SigNoz creates its org and root admin at boot and keeps them across restarts and `--reuse` runs. The suite logs in as that user, gives a service account the `signoz-admin` role, and issues it an API key. A real `ProviderConfig` carries the same kind of key. The key is revoked when the session ends.

## Namespaces

The operator watches every namespace. All tests share `signoz-operator-e2e`, and each test names its resources uniquely. The manager runs in `signoz-operator-system`, where `config/default` installs it.

## Layout

- `casting.yaml`: Foundry's installation definition.
- `kind.yaml`: the Kind cluster and its host port mapping.
- `fixtures/`: all fixtures and shared helpers, registered in `conftest.py`.
- `e2e/operator/`: an overlay that points `config/default` at the locally built image.
- `e2e/signoz/service.yaml`: the NodePort that publishes the SigNoz API.
- `e2e/bootstrap/setup.py`: brings the environment up, and runs first as a smoke check.
- `e2e/tests/<suite>/NN_*.py`: the tests.
- `testdata/`: request bodies sent to SigNoz.
