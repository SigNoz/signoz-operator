<p align="center">
  <a href="https://signoz.io" target="_blank">
    <img alt="SigNoz" src="https://signoz.io/img/SigNozLogo-orange.svg" width="120">
  </a>
</p>

<h1 align="center" style="border-bottom: none">SigNoz Operator</h1>

<p align="center">
  <a href="https://github.com/SigNoz/signoz-operator/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/SigNoz/signoz-operator?include_prereleases"></a>
  <a href="https://golang.org"><img alt="Go Version" src="https://img.shields.io/badge/Go-1.26+-blue.svg"></a>
  <a href="LICENSE"><img alt="License: AGPL v3" src="https://img.shields.io/badge/License-AGPL%20v3-blue.svg"></a>
  <a href="https://github.com/SigNoz/signoz-operator/issues"><img alt="GitHub issues" src="https://img.shields.io/github/issues/SigNoz/signoz-operator"></a>
  <a href="https://signoz.io/slack"><img alt="Slack" src="https://img.shields.io/badge/Slack-SigNoz-4A154B?logo=slack"></a>
</p>

<p align="center">Manage SigNoz dashboards, alert rules, users and more as Kubernetes custom resources.</p>

<h3 align="center">
  <a href="https://signoz.io/docs/"><b>SigNoz Documentation</b></a> &bull;
  <a href="https://signoz.io/teams/"><b>SigNoz Cloud</b></a> &bull;
  <a href="https://signoz.io/slack"><b>Slack</b></a> &bull;
  <a href="https://signoz.io"><b>Website</b></a>
</h3>

## Overview

The SigNoz Operator manages the contents of a [SigNoz](https://signoz.io) instance from Kubernetes. Dashboards, alert rules, users, roles and other SigNoz objects are declared as custom resources. The operator creates them through the SigNoz API, re-checks them periodically to correct drift, and cleans them up when the custom resource is deleted.

The operator talks to SigNoz over HTTP, so the instance it manages does not have to run in the same cluster, or in Kubernetes at all. A single operator can manage several SigNoz instances, cloud or self-hosted; each resource selects the instance it belongs to.

The operator does not deploy SigNoz itself. To run SigNoz on Kubernetes, use the [SigNoz Helm chart](https://signoz.io/docs/install/kubernetes/). This operator manages the configuration inside an existing SigNoz instance.

## Features

- **Observability as code**: dashboards and alert rules are versioned in Git alongside the services they monitor, and reviewed and rolled back like any other change.
- **GitOps**: managed resources are ordinary custom resources, so tools such as Argo CD and Flux handle them without plugins or custom sync logic.
- **Drift correction**: the operator re-checks each resource on an interval and reverts changes made outside Kubernetes, such as edits in the SigNoz UI.
- **Multiple instances**: each resource references a `ProviderConfig`, so one operator can manage staging and production, or cloud and self-hosted, at the same time.
- **Typed or raw specs**: objects can be written as typed, schema-validated fields, or as the JSON request body SigNoz already accepts.

## Getting Started

### Install

Install the operator with the [Helm chart](https://github.com/SigNoz/charts/tree/main/charts/signoz-operator), which includes the CRDs, the RBAC and the controller:

```bash
helm repo add signoz https://charts.signoz.io
helm install signoz-operator signoz/signoz-operator \
  --namespace signoz-operator-system --create-namespace
```

Alternatively, apply the rendered manifest published with each release:

```bash
kubectl apply -f https://github.com/SigNoz/signoz-operator/releases/latest/download/signoz-operator.yaml
```

A separate `signoz-operator.crds.yaml` is published with each release for clusters that manage CRDs on their own.

Both methods install the operator into the `signoz-operator-system` namespace. Verify that it is running:

```bash
kubectl -n signoz-operator-system rollout status deployment/signoz-operator
```

### Connect the operator to SigNoz

A `ProviderConfig` defines a SigNoz endpoint and the credentials used to authenticate to it. Its Secret and ConfigMap references are resolved in its own namespace, so create them together:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: signoz-api
type: Opaque
stringData:
  token: <your-signoz-api-key>
---
apiVersion: resources.signoz.io/v1alpha1
kind: ProviderConfig
metadata:
  name: prod
spec:
  endpoint:
    value: https://eu.signoz.cloud
  auth:
    header:
      # name defaults to SIGNOZ-API-KEY
      valueFrom:
        secretKeyRef:
          name: signoz-api
          key: token
```

### Create a resource

Every managed kind has the same shared fields at the root of its spec, with the SigNoz object itself under `objectTemplate`:

```yaml
apiVersion: resources.signoz.io/v1alpha1
kind: Dashboard
metadata:
  name: service-overview
spec:
  providerConfigRef:
    name: prod
  interval: 5m
  objectTemplate:
    spec:
      name: service-overview
      schemaVersion: v2
      tags:
      - key: team
        value: platform
      spec:
        display:
          name: Service overview
          description: Golden signals for the demo service
        panels: {}
        layouts: []
        variables: []
```

Apply it and check the status:

```bash
kubectl get dashboards
```

```
NAME               READY   REASON    ID                                     AGE
service-overview   True    Created   0198c0e1-4f2a-7c9e-b3d5-6a1f8e2d4c07   12s
```

The `Ready` condition summarizes the others, so `kubectl wait --for=condition=Ready dashboard/service-overview` is sufficient. Samples for every kind are in [`config/samples/`](config/samples/).

## Custom Resources

All kinds are in the `resources.signoz.io/v1alpha1` API group.

### Backends

| Kind | Scope | Description |
|---|---|---|
| `ProviderConfig` | Namespaced | A SigNoz endpoint and the credentials for it. Secret and ConfigMap references are resolved in its own namespace. |
| `ClusterProviderConfig` | Cluster | The same spec, cluster-scoped. References are resolved in the operator's namespace, so one credential can be shared across namespaces. |

A resource selects a backend with `spec.providerConfigRef`; its `kind` field picks between the two and defaults to `ProviderConfig`. There is no namespace field: a namespaced `ProviderConfig` can only be referenced from its own namespace.

### Managed objects

Each of these kinds mirrors one SigNoz object. The identity column is the field the operator uses to match an existing object in SigNoz instead of creating a new one.

| Kind | SigNoz object | Identified by |
|---|---|---|
| `Dashboard` | Dashboard | `name` |
| `Rule` | Alert rule | `alert` |
| `SavedView` | Saved view | `name` |
| `PlannedMaintenance` | Downtime schedule | `name` |
| `RoutePolicy` | Notification route policy | `name` |
| `User` | User | `email` |
| `Role` | Role | `name` |
| `ServiceAccount` | Service account | `name` |
| `AuthDomain` | Auth domain (SSO) | `name` |

### Shared spec

Every managed kind has the same fields alongside its required `objectTemplate`:

| Field | Default | Description |
|---|---|---|
| `providerConfigRef` | *required* | The `ProviderConfig` or `ClusterProviderConfig` to use. |
| `interval` | `10m` | How often the resource is re-checked against SigNoz. |
| `retryInterval` | `interval` | How soon a recoverable failure is retried. |
| `timeout` | `30s` | Timeout for one reconcile attempt, including calls to SigNoz. |
| `suspend` | `false` | Pause reconciliation without changing or deleting anything in SigNoz. |
| `reclaimPolicy` | `Delete` | Whether the SigNoz object is deleted (`Delete`) or kept (`Orphan`) when the custom resource is deleted. |

`objectTemplate` carries the SigNoz object in exactly one of two forms:

- **`spec`**: typed fields, validated against the CRD schema by the API server at apply time.
- **`jsonSpec`**: the SigNoz request body as a JSON string, sent as-is. Useful for applying an object exported from the SigNoz UI, or for setting fields the typed schema does not include yet.

To bring an object that already exists in SigNoz under management, annotate the resource with `resources.signoz.io/signoz-resource-id: <id>`; the operator adopts that object instead of creating a new one.

### Status

Every kind reports the same set of conditions, with per-kind detail in the reason and message:

| Condition | Meaning |
|---|---|
| `Ready` | Derived from the other conditions. The one users and tools should wait on. |
| `Synced` | `True` when SigNoz matches the desired state, `False` when it cannot, `Unknown` when the operator could not tell. |
| `Terminal` | The spec is invalid and retrying will not help. The operator stops requeueing until the spec changes. |
| `Recoverable` | A transient failure, such as a rate limit, a server error or a connection error. Retried at `retryInterval`. |
| `Suspended` | Reconciliation is paused by `spec.suspend`. |

`status.signozResource.id` records the object's ID in SigNoz. `status.observedGeneration`, `status.observedHash` and `status.reconciledAt` record what was last reconciled and when.

## Configuration

Every flag can also be set as an environment variable: prefix it with `SIGNOZ_OPERATOR_`, upper-case it and replace dashes with underscores, so `--log-level` becomes `SIGNOZ_OPERATOR_LOG_LEVEL`. Command-line flags take precedence over environment variables.

```
signoz-operator [flags]

Flags:
  --operator-namespace string              Namespace the operator runs in. A ClusterProviderConfig's Secret
                                           and ConfigMap references resolve here. Required.
  --watch-namespaces strings               Namespaces to watch. Defaults to all namespaces.
  --log-level string                       One of 'debug', 'info', 'error', 'panic' (default "info")
  --leader-elect                           Enable leader election, so only one manager is active
  --default-resources-interval duration    Reconcile cadence when a resource omits .spec.interval (default 10m0s)
  --default-resources-retry-interval duration
                                           Retry cadence when a resource omits .spec.retryInterval (default 1m0s)
  --default-resources-timeout duration     Bound on one attempt when a resource omits .spec.timeout (default 30s)
  --metrics-bind-address string            Metrics address; ":8443" for HTTPS, ":8080" for HTTP, "0" to disable (default "0")
  --metrics-secure                         Serve metrics over HTTPS (default true)
  --health-probe-bind-address string       Address the health and readiness probes bind to (default ":8081")
  --enable-http2                           Enable HTTP/2 on the metrics and webhook servers
```

Run `signoz-operator --help` for the full list, including the certificate options for the metrics and webhook servers.

## Development

Development requires [Go 1.26+](https://golang.org/dl/) and SigNoz primus, which provides the shared build targets. Set `PRIMUS_HOME` to a checkout of primus.

```bash
make checks                # everything below
make go-checks             # fmt, deps, lint, test
make controllergen-checks  # regenerate deepcopy, CRDs and RBAC
make kyaml-checks          # format the YAML manifests
```

The API types, CRDs and RBAC are generated. After editing anything under `api/` or any `+kubebuilder:` marker, run `make controllergen-checks` and commit the result; CI regenerates them and fails if there is a diff. Do not edit `config/crd/bases/`, `config/rbac/clusterrole.generated.yaml` or `zz_generated.*.go` by hand.

The operator is built against Kubernetes `v0.36` client libraries and controller-runtime `v0.24`.

## Contributing

Contributions are welcome. Open an [issue](https://github.com/SigNoz/signoz-operator/issues) or a pull request to get started, or ask in the `#contributing` channel on the [SigNoz Slack](https://signoz.io/slack). The design docs live in [`docs/`](docs/).

## Community

Join the [SigNoz Slack](https://signoz.io/slack) to ask questions and connect with other users and contributors. For ideas and feedback, use [GitHub Discussions](https://github.com/SigNoz/signoz/discussions).

## License

Licensed under the [GNU Affero General Public License v3.0](LICENSE).
