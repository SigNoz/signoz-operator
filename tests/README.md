# Operator tests

The E2E suite creates an isolated [Kind](https://kind.sigs.k8s.io/) cluster,
installs SigNoz through `foundryctl`, builds the current checkout's operator
image, loads it into Kind, and applies the operator Kustomize overlay. It then drives the operator against that real SigNoz:
applying custom resources, reading back what appeared in SigNoz over its HTTP
API, and asserting the conditions the operator reported.

## Requirements

- Docker
- Kind
- kubectl
- foundryctl
- Python 3.11+ and uv

## Run

From the repository root:

```sh
make test-e2e
```

The default run removes the cluster and Foundry-generated files after the suite.
For local development, leave the environment running:

```sh
make test-e2e-reuse
kubectl --kubeconfig tests/tmp/kubeconfig get pods --all-namespaces
make cleanup-test-e2e
```

`make setup-e2e-env` brings the environment up without running the behaviour
tests. Narrow a run to one file or test with `E2E_TARGET`:

```sh
make test-e2e-reuse E2E_TARGET=e2e/tests/test_dashboard.py
make test-e2e-reuse E2E_TARGET=e2e/tests/test_dashboard.py::test_a_change_made_in_signoz_is_reverted
```

The retained environment is recorded in pytest's cache, so `--reuse` only
connects to a cluster this suite created. A non-reuse run refuses to take over
an existing cluster with the configured name.

## How the suite reaches SigNoz

The operator talks to SigNoz in-cluster, at
`http://signoz-signoz.signoz.svc.cluster.local:8080`. The test process needs the
same API from the host to check what the operator actually wrote, so
`e2e/signoz/service.yaml` publishes it on a NodePort that `kind.yaml` maps to
`127.0.0.1:30080`. Those two ports and the constant in `fixtures/signoz.py` are
one setting in three places.

The suite waits on `/api/v2/healthz`, which is open access and so answers before
it holds a credential.

`casting.yaml` sets `SIGNOZ_USER_ROOT_*`, so SigNoz provisions its org and root
admin at boot and reconciles them on every restart. The suite logs in as that
user, grants a service account the `signoz-admin` role, and issues it an API
key — the same kind of credential a real ProviderConfig carries. It sets
`SIGNOZ_STATSREPORTER_ENABLED=false` as well, since nothing about a throwaway
cluster is worth reporting upstream.

The two boolean settings are `spec.patches` rather than `spec.signoz.spec.env`
because Foundry's env template renders `value: {{ $val }}`, so `"true"` reaches
the API server as a boolean, which a container's env value cannot hold. A patch
carries an object, so its value stays a string.

The installation is otherwise Foundry's default, with one override: the
clickhouse-operator config Foundry ships defines an engine for `query_log`, and
ClickHouse refuses a system table that carries both an engine and
`partition_by`, so `spec.telemetrystore` deletes the `partition_by` Foundry
generates. Without it ClickHouse never finishes loading metadata.

## Namespaces

The shipped deployment watches every namespace and binds a ClusterRole, so a
mirrored resource is reconciled wherever it is applied. The tests work in
`signoz-operator-e2e`, deliberately not the operator's own namespace, isolated
by a name unique to the test rather than by a namespace of their own.

`config/default` installs the operator into `signoz-operator-system`, so that is
where the manager runs and where a `ClusterProviderConfig`'s Secret and
ConfigMap references resolve.

## Layout

- `casting.yaml` is Foundry's Kubernetes/Kustomize installation definition.
- `kind.yaml` defines the Kind cluster topology and the port mapping that
  publishes the SigNoz API to the host.
- `fixtures/` contains lifecycle fixtures for Kind, Foundry, kubectl, the
  operator, the SigNoz API client, and their composed `Environment`.
- `e2e/operator/kustomization.yaml` overlays the repository deployment manifests
  and points them at the locally built test image. The base already pulls
  `IfNotPresent`, so Kind uses the image loaded into it.
- `e2e/signoz/service.yaml` publishes the installed SigNoz API to the host.
- `e2e/bootstrap/setup.py` is the explicit setup entrypoint, and runs first as a
  smoke check.
- `e2e/tests/` contains the operator behaviour tests, and `e2e/tests/conftest.py`
  the fixtures that wire resources up.
- `testdata/` holds the SigNoz request bodies those tests send.
