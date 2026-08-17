# Core spec

> The controls every kind shares are typed spec fields, validated by the API server and defaulted by the schema.

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

**Rejected.** The ground is what annotations *are*. The Kubernetes API conventions draw the line exactly at our case: `.spec` is *"a complete description of the desired state, including configuration settings provided by the user"*, whereas annotations are for metadata *"the controller responsible for the resource doesn't need to know about"* ([Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)). A value our own controller consumes is desired state by that definition, and the same document forecloses the annotation route for new fields directly: *"no new annotations may be defined. New API fields are now developed as regular fields."*

The concrete cost follows from annotation values being untyped. Annotation values *"must be strings"* ([Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)) and sit outside the CRD's structural schema, so they are not type-checked, not defaulted, not shown by `kubectl explain`, and are pruned rather than validated. KEDA is the worked example: it exposes pause as the annotation `autoscaling.keda.sh/paused`, with a companion `autoscaling.keda.sh/paused-replicas` documented as taking `"<number>"` ([KEDA — pause autoscaling](https://keda.sh/docs/latest/concepts/scaling-deployments/)). Because that replica count is an unvalidated string, a malformed value is accepted by the API server at apply time and only fails later, deep inside the reconcile loop, where it leaves the object stuck. That is the general failure of putting a typed knob in an untyped annotation — the error moves from a clean `kubectl apply` rejection to a silent, hard-to-trace reconcile-time fault.

### Typed spec fields

Declare the controls as fields on `CoreSpec`, with kubebuilder validation and defaulting markers.

**Accepted.** This is what the desired-state definition asks for, and it is the settled upstream idiom for exactly these knobs. Kubernetes models a reconciliation toggle as a spec boolean — Job's `.spec.suspend` *"specifies whether the Job controller should create Pods or not"* ([Job API](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/job-v1/)). Flux models cadence as the spec durations `.spec.interval`, `.spec.retryInterval` — *"exclusively meant for failure retries"*, defaulting to `.spec.interval` — and `.spec.timeout`, *"a timeout duration for any operation … performed during the reconciliation process"* ([Flux Kustomization](https://fluxcd.io/flux/components/kustomize/kustomizations/)). Kubernetes models deletion behaviour as a spec enum, in `PersistentVolume.spec.persistentVolumeReclaimPolicy` ([Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)). In every case the value is typed, server-validated, defaultable, and discoverable through the published schema.

## Decision

`CoreSpec` holds these typed fields. The three durations follow Flux's `interval` / `retryInterval` / `timeout` model. `reclaimPolicy` is defaulted by the CRD schema; the three durations are defaulted by the controller from operator flags when omitted, so a cluster administrator can move the whole install's cadence at once.

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
| `timeout` | `*metav1.Duration` | Upper bound on a single reconciliation attempt, including the HTTP calls to SigNoz. Omitted, it falls back to the operator-wide default (`1m` unless the flag changes it). Each resource owns its own bound. |
| `suspend` | `bool` | When true, the operator stops reconciling this resource and reports the `Suspended` condition; nothing in SigNoz is changed or deleted. |
| `reclaimPolicy` | `ReclaimPolicy` (`Delete`/`Orphan`) | On custom-resource deletion, `Delete` removes the SigNoz object, `Orphan` leaves it. Defaults to `Delete`. |

## Consequences

- A bad value is rejected at `kubectl apply`, by the API server against the CRD schema. An unparseable `interval` or a misspelled `reclaimPolicy` never reaches the controller at all.

- The knobs are self-documenting: `kubectl explain dashboard.spec` lists them with their types and defaults, and `reclaimPolicy` defaults to `Delete` through the schema with no controller code. A user discovers the controls from the object itself.

- Timeout is a property of the resource, not of the connection. Two resources sharing one `ProviderConfig` can carry different bounds, and a slow SigNoz endpoint is bounded per resource. The cost is that a cluster administrator who wants one bound for a whole backend has no single place to set it — only the operator flag, which covers every backend at once.

- Changing `suspend` or `reclaimPolicy` is a spec edit, so under GitOps it takes a commit and a reconcile. There is no supported way to pause a resource out of band, which is a real cost during an incident.

- Cluster-wide behaviour is settable at two scopes only: per object, in the spec, and per operator, by flag. There is no namespace-scoped default. Adding one later is additive — a default resolved by the controller — and needs no schema change.

- The dividing line between `.spec` and `metadata` is declared intent versus operator bookkeeping. User intent goes in `.spec`; the operator's own record of a reconcile — the create-attempt annotation in [idempotency.md](idempotency.md), which the user never sets and which must survive when status is dropped — goes in an annotation.

## Sources

- [Kubernetes API conventions — spec and status, annotation conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [Kubernetes — Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
- [Kubernetes — Job API reference (`spec.suspend`)](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/job-v1/)
- [Kubernetes — Persistent Volumes (reclaim policy)](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
- [Flux — Kustomization (`interval`, `retryInterval`, `timeout`)](https://fluxcd.io/flux/components/kustomize/kustomizations/)
- [KEDA — Scaling Deployments (pause annotations)](https://keda.sh/docs/latest/concepts/scaling-deployments/)
