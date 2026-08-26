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

<p align="center"><b>Your SigNoz dashboards, alerts and users, as Kubernetes objects.</b></p>

<p align="center">Declare what SigNoz should contain in YAML, apply it, and let the operator keep SigNoz matching it.</p>

<h3 align="center">
  <a href="docs/"><b>Design docs</b></a> &bull;
  <a href="https://signoz.io/docs/"><b>SigNoz Documentation</b></a> &bull;
  <a href="https://signoz.io/teams/"><b>SigNoz Cloud</b></a> &bull;
  <a href="https://signoz.io/slack"><b>Slack</b></a> &bull;
  <a href="https://signoz.io"><b>Website</b></a>
</h3>

## Overview

The SigNoz Operator manages the contents of a SigNoz instance from Kubernetes. A dashboard, an alert rule, a user or a role is written as a custom resource; the operator reconciles it against the SigNoz API and keeps the two in step — creating the object on apply, correcting drift on a schedule, and applying a reclaim policy on delete.

It talks to SigNoz over HTTP, so the SigNoz it manages does not have to run in the same cluster, or in Kubernetes at all. One operator can drive SigNoz Cloud, a self-hosted instance, or several of each, chosen per resource.

**It does not deploy SigNoz.** Installing and running SigNoz itself is the job of the [SigNoz Helm chart](https://signoz.io/docs/install/kubernetes/) — this operator manages what lives *inside* a SigNoz that already exists.

## About SigNoz

[SigNoz](https://signoz.io) is an open-source observability platform built on OpenTelemetry, with logs, metrics, traces, alerts and dashboards in one place.

- Get started with [SigNoz Cloud](https://signoz.io/teams/) for free, or [self-host SigNoz](https://signoz.io/docs/install/self-host/).
- Star and explore the main project at [SigNoz/signoz](https://github.com/SigNoz/signoz).

## Why the SigNoz Operator?

- **Observability as code**: dashboards and alert rules live in Git next to the services they watch, reviewed and rolled back like any other change.
- **GitOps-native**: plain custom resources, so Argo CD and Flux manage them with no plugin and no bespoke sync logic.
- **Drift is corrected, not just detected**: the operator re-checks each resource on an interval and rewrites SigNoz when someone has edited the object in the UI.
- **Many backends, one operator**: each resource names the SigNoz it writes to, so staging and production, or cloud and self-hosted, are a field — not a second deployment.
- **Typed or verbatim**: author an object as validated fields, or paste the JSON body SigNoz already understands. Both are first-class.

## Getting Started

### Install

The [chart](https://github.com/SigNoz/charts/tree/main/charts/signoz-operator) tracks each operator release and installs the CRDs, the RBAC and the controller:

```bash
helm repo add signoz https://charts.signoz.io
helm install signoz-operator signoz/signoz-operator \
  --namespace signoz-operator-system --create-namespace
```

Every release also carries a rendered manifest, for clusters that install with plain `kubectl`:

```bash
kubectl apply -f https://github.com/SigNoz/signoz-operator/releases/latest/download/signoz-operator.yaml
```

`signoz-operator.crds.yaml` is published alongside it for clusters that manage CRDs separately.

Either way, the operator lands in `signoz-operator-system`. Check it is up:

```bash
kubectl -n signoz-operator-system rollout status deployment/signoz-operator
```

### Point the operator at a SigNoz

A `ProviderConfig` names one SigNoz backend and how to authenticate to it. Its Secret and ConfigMap references resolve in its own namespace, so keep the two together:

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

### Declare a resource

Every managed kind carries the same shared controls at the root of its spec, and the SigNoz object itself under `objectTemplate`:

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

Apply it, and watch it land:

```bash
kubectl get dashboards
```

```
NAME               READY   REASON    ID                                     AGE
service-overview   True    Created   0198c0e1-4f2a-7c9e-b3d5-6a1f8e2d4c07   12s
```

`Ready` is the condition to wait on — it rolls up everything else, so `kubectl wait --for=condition=Ready dashboard/service-overview` is enough. More samples for every kind are in [`config/samples/`](config/samples/).

## Custom Resources

All kinds live in the `resources.signoz.io/v1alpha1` API group.

### Backends

| Kind | Scope | What it is |
|---|---|---|
| `ProviderConfig` | Namespaced | One SigNoz endpoint and credential. Secret and ConfigMap references resolve in its own namespace. |
| `ClusterProviderConfig` | Cluster | The same spec, cluster-wide. Its references resolve in the operator's namespace, so a credential is shared without copying it into every namespace. |

A resource selects one with `spec.providerConfigRef`, whose `kind` field picks between the two and defaults to `ProviderConfig`. There is no namespace field — a namespaced provider config is only usable from its own namespace, by design.

### Managed objects

Each of these mirrors one SigNoz object. The identity column is the field the operator matches on when it has to find an existing object rather than create one.

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

Every managed kind inlines the same controls, alongside its required `objectTemplate`:

| Field | Default | What it does |
|---|---|---|
| `providerConfigRef` | *required* | Which SigNoz to write through. |
| `interval` | `10m` | How often the resource is re-checked against SigNoz. |
| `retryInterval` | `interval` | How soon a recoverable failure is retried. |
| `timeout` | `30s` | Upper bound on one reconcile attempt, SigNoz calls included. |
| `suspend` | `false` | Stop reconciling without changing or deleting anything in SigNoz. |
| `reclaimPolicy` | `Delete` | What happens in SigNoz when the custom resource is deleted — `Delete` or `Orphan`. |

`objectTemplate` carries the SigNoz object in exactly one of two forms:

- **`spec`** — typed fields, validated by the API server against the CRD schema at apply time.
- **`jsonSpec`** — the SigNoz request body as a JSON string, sent verbatim. Use it to apply an object exported from the SigNoz UI unchanged, or to set a field the typed form does not carry yet.

To bring an object that already exists in SigNoz under management, annotate the resource with `resources.signoz.io/signoz-resource-id: <id>`; the operator adopts that object instead of creating a new one.

### Status

Status reports through a fixed condition vocabulary, the same on every kind, with per-kind detail in the reason and message:

| Condition | Meaning |
|---|---|
| `Ready` | Derived from the others, reporting the most serious that applies. The only one a user or tool should wait on. |
| `Synced` | Three-valued: `True` when SigNoz matches desired state, `False` when it never will, `Unknown` when the operator could not tell. |
| `Terminal` | The spec is wrong and no retry will help. A stable state — the operator settles here and stops requeueing. |
| `Recoverable` | A transient failure — a 429, a 5xx, a connection error. Retried at `retryInterval`. |
| `Suspended` | Paused by `spec.suspend`. |

`status.signozResource.id` records the object's identity in SigNoz, and `status.observedGeneration`, `status.observedHash` and `status.reconciledAt` describe what was last reconciled and when.

For the reasoning behind all of this, see [`docs/`](docs/): [`core-spec.md`](docs/core-spec.md), [`core-status.md`](docs/core-status.md), [`resources.md`](docs/resources.md), [`provider-config.md`](docs/provider-config.md) and [`idempotency.md`](docs/idempotency.md).

## Configuration

Every flag can also be set as an environment variable, prefixed with `SIGNOZ_OPERATOR_` and upper-cased with dashes as underscores — `--log-level` is `SIGNOZ_OPERATOR_LOG_LEVEL`. A flag passed on the command line wins over the environment.

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

Run `signoz-operator --help` for the full list, including the certificate paths for the metrics and webhook servers.

## Development

Requires [Go 1.26+](https://golang.org/dl/) and SigNoz primus, which provides the shared build targets — set `PRIMUS_HOME` to your checkout of it.

```bash
make checks                # everything below
make go-checks             # fmt, deps, lint, test
make controllergen-checks  # regenerate deepcopy, CRDs and RBAC
make kyaml-checks          # format the YAML manifests
```

The API types, CRDs and RBAC are generated. After editing anything under `api/`, or any `+kubebuilder:` marker, run `make controllergen-checks` and commit the result — CI regenerates them and fails on a diff. Never hand-edit `config/crd/bases/`, `config/rbac/clusterrole.generated.yaml` or `zz_generated.*.go`.

Built against Kubernetes `v0.36` client libraries and controller-runtime `v0.24`.

## Contributing

We ❤️ contributions big or small. Open an [issue](https://github.com/SigNoz/signoz-operator/issues) or a pull request to get started. Not sure where to begin? Just ping us on `#contributing` in our [Slack community](https://signoz.io/slack).

## Community

Come say Hi to us on [Slack](https://signoz.io/slack) 👋 to talk observability, OpenTelemetry and SigNoz, and to connect with other users and contributors. If you have ideas, questions or feedback, share them on [GitHub Discussions](https://github.com/SigNoz/signoz/discussions).

## License

Licensed under the [GNU Affero General Public License v3.0](LICENSE).
