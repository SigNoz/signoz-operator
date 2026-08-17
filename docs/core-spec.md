# Core spec

> The controls every kind shares are typed spec fields, because the operator reads them as desired state — not annotations.

## Context

Every resource this operator manages carries the same handful of controls, independent of what the resource is: which SigNoz to write to, how often to re-check it, how soon to retry a failure, how long a single attempt may run, whether to pause reconciliation, and what to do with the remote object when the custom resource is deleted. Rather than redeclare these on every kind, they live in one struct, `CoreSpec`. Its observed-state counterpart is [core-status.md](core-status.md).

The open question these fields raise is not what they are but where they belong: a typed field under `.spec`, or a string under `metadata.annotations`. Both work mechanically, and real operators have gone each way — so the choice has to be made on the properties each location gives.

## Constraints

- **Every one of these controls is read by this operator's own reconcile loop.** The provider-config reference decides where a write goes; the interval decides when the next reconcile runs; the retry interval decides how soon a failed one runs again; the timeout bounds a single attempt; suspend decides whether to act at all; the reclaim policy decides what a finalizer does. None is metadata for some other tool — they are inputs to *our* controller's decisions.

- **Most of them are typed, not free strings.** The interval, retry interval, and timeout are durations, the reclaim policy is a closed enum, suspend is a boolean. A wrong value for any of them — an unparseable duration, a misspelled policy — should be caught, and caught early.

Whatever holds these values therefore has to type and validate them, has to admit a default, and has to be legible to the tools a user already points at the object.

## Considered options

### Annotations

Carry each control as a `resources.signoz.io/...` annotation on the custom resource, read by the controller during reconcile.

**Rejected**, on what annotations *are*. The Kubernetes API conventions draw the line exactly at our case: `.spec` is *"a complete description of the desired state, including configuration settings provided by the user"*, whereas annotations are for metadata *"the controller responsible for the resource doesn't need to know about"* ([Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)). A value our own controller consumes is desired state by that definition, and the same document forecloses the annotation route for new fields directly: *"no new annotations may be defined. New API fields are now developed as regular fields."*

The concrete cost follows from annotation values being untyped. Annotation values *"must be strings"* ([Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)) and sit outside the CRD's structural schema, so they are not type-checked, not defaulted, not shown by `kubectl explain`, and are pruned rather than validated. KEDA, which exposes pause as the annotation `autoscaling.keda.sh/paused` (with a companion `autoscaling.keda.sh/paused-replicas`), is the worked example: because the replica count is an unvalidated string, a malformed value is accepted by the API server at apply time and only fails later, deep inside the reconcile loop, where it leaves the object stuck. That is the general failure of putting a typed knob in an untyped annotation — the error moves from a clean `kubectl apply` rejection to a silent, hard-to-trace reconcile-time fault.

### Typed spec fields

Declare the controls as fields on `CoreSpec`, with kubebuilder validation and defaulting markers.

**Accepted.** This is what the desired-state definition asks for, and it is the settled upstream idiom for exactly these knobs. Kubernetes itself models a reconciliation toggle as a spec boolean — CronJob and Job both use `.spec.suspend` ([Job API](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/job-v1/)) — Flux models cadence as the spec durations `.spec.interval` / `.spec.retryInterval` / `.spec.timeout`, and both Crossplane (`.spec.deletionPolicy`, `Delete`/`Orphan`) and core Kubernetes (`PersistentVolume.spec.persistentVolumeReclaimPolicy`, `Retain`/`Delete`) model deletion behaviour as a spec enum. In every case the value is typed, server-validated, defaultable, and discoverable through the published schema.

## Decision

`CoreSpec` holds these typed fields. The three durations follow Flux's `interval` / `retryInterval` / `timeout` model. Cluster-wide defaults (if any) come from operator flags.

```go
type CoreSpec struct {
    // Which SigNoz to write to. Required — every resource names its backend; there is no operator default.
    // +kubebuilder:validation:Required
    ProviderConfigRef ProviderConfigReference `json:"providerConfigRef"`

    // Interval is the steady-state cadence at which the resource is re-checked
    // against SigNoz. Defaults to the operator default.
    // +optional
    Interval *metav1.Duration `json:"interval,omitempty"`

    // RetryInterval is the cadence at which a failed reconciliation is retried.
    // +optional
    RetryInterval *metav1.Duration `json:"retryInterval,omitempty"`

    // Timeout bounds a single reconciliation attempt, including the HTTP calls
    // to SigNoz.
    // +optional
    Timeout *metav1.Duration `json:"timeout,omitempty"`

    // Suspend stops reconciling this resource without deleting anything.
    // +optional
    Suspend bool `json:"suspend,omitempty"`

    // ReclaimPolicy controls what happens in SigNoz when this custom resource
    // is deleted.
    // +kubebuilder:validation:Enum=Delete;Orphan
    // +kubebuilder:default=Delete
    // +optional
    ReclaimPolicy ReclaimPolicy `json:"reclaimPolicy,omitempty"`
}
```

| Field | Type | Behaviour |
|---|---|---|
| `providerConfigRef` | `ProviderConfigReference` | **Required.** Names the `ProviderConfig` (same namespace) or cluster-scoped `ClusterProviderConfig` to write through, by `name` and `kind` — see [provider-config.md](provider-config.md). There is no operator-wide default; a single-tenant install creates one object and names it. |
| `interval` | `*metav1.Duration` | Steady-state cadence at which the operator re-checks the remote object against desired state, driving the periodic drift correction described in [core-status.md](core-status.md). Omitted, it falls back to the operator-wide default. |
| `retryInterval` | `*metav1.Duration` | Cadence at which a `Recoverable` failure is retried, letting failures back off on a shorter loop than the steady `interval`. A fixed cadence, not exponential. |
| `timeout` | `*metav1.Duration` | Upper bound on a single reconciliation attempt, including the HTTP calls to SigNoz. Defaults to `1m`. Each resource owns its own, so there is no competing connection-wide or operator-wide reconcile timeout. |
| `suspend` | `bool` | When true, the operator stops reconciling this resource and reports the `Suspended` condition; nothing in SigNoz is changed or deleted. |
| `reclaimPolicy` | `ReclaimPolicy` (`Delete`/`Orphan`) | On custom-resource deletion, `Delete` removes the SigNoz object, `Orphan` leaves it. Defaults to `Delete`. |

## Consequences

- A bad value is rejected at `kubectl apply`, by the API server against the CRD schema, not discovered later inside a reconcile. An unparseable `interval` or a misspelled `reclaimPolicy` never reaches the controller, which is the failure mode the annotation route produces and cannot prevent.

- The knobs are self-documenting: `kubectl explain dashboard.spec` lists them with their types and defaults, and `reclaimPolicy` defaults to `Delete` through the schema with no controller code. A user discovers the controls from the object itself.

- Timeout is a property of the resource, not of the connection. Keeping any timeout field off the `ProviderConfig` removes a knob that could compete with this one: a slow SigNoz endpoint is bounded per resource, by the resource's own `timeout`, so two resources sharing a `ProviderConfig` can carry different bounds and a shared `ProviderConfig` never imposes a single global limit.

- Changing `suspend` or `reclaimPolicy` is a spec edit. Under GitOps that means a commit, not a quick `kubectl annotate` — the one ergonomic advantage the annotation approach had, which we give up deliberately in exchange for typing, defaulting, and consistency with the rest of the API.

- Cluster-wide behaviour is settable, but only at two scopes — per object (spec) and whole operator (flag). There is no namespace-scoped default. If that need ever appears, it is an additive change — a namespace-scoped default resolved by the controller — not a reason to move the per-object value into an annotation.

- This does not contradict the operator's use of a `metadata` annotation for the create-attempt marker in [idempotency.md](idempotency.md). That marker is operator bookkeeping the user never sets and that must survive when status is dropped; it is metadata about the reconcile, not declared intent. The dividing line holds in both documents: user intent goes in `.spec`, operator bookkeeping goes in an annotation.

## Sources

- [Kubernetes API conventions — spec and status, annotation conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [Kubernetes — Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
- [Kubernetes — Job API reference (`spec.suspend`)](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/job-v1/)
