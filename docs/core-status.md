# Core status

> Report what we observed, distinguish a transient failure from a permanent one, and expose one condition worth waiting on.

## Context

The operator keeps reconciling towards a desired state (in SigNoz) held in a custom resource (in K8s). `.status` is the operator's report back: what it observed about the remote object, whether the two are in sync, and - when they are not - why. It is the only channel a user or a GitOps tool has for that information, because the operator never writes anything back into `.spec` and never annotates the remote object. Running `kubectl get` or `kubectl wait` against the CR is the whole interface.

Status is written under a status subresource (`+kubebuilder:subresource:status`), so a spec edit and a status update are separate writes and cannot race each other. A reader needs three things from it: did the last reconcile succeed, is the resource in a state that will change on its own or one that is stuck until a human acts, and which remote object does this CR correspond to. A flat "ready / not ready" answers the first and none of the rest, and in particular cannot tell a caller whether waiting will help.

The remote object is created over HTTP with a server-assigned identifier, so the identifier is observed state discovered after the create, not desired state the user supplies — see [idempotency.md](idempotency.md). Status therefore has to carry that identifier once it is known, and has to be honest about the window in which it is not yet known.

The desired-state half of the same shared struct — the connection reference, reconcile interval, suspend switch, and reclaim policy — is [core-spec.md](core-spec.md); this document covers only the observed-state half.

## Constraints

- **Condition *types* must be uniform across every kind.** This is a choice, not a platform limit. `kubectl wait --for=condition=Ready`, GitOps health checks, and every generic dashboard key off a condition type string; if `Dashboard` reports `DashboardSynchronized` and `Rule` reports `RuleSynchronized`, none of those tools work across kinds. Per-kind detail **must not** go in the type — it goes in the `reason` and `message`. The type vocabulary is fixed and shared.

- **Status must not be load-bearing for identity.** Status is a subresource and there are real paths where the object survives without it — a restore from a backup that excludes status is the common one. So the *guarantee* against creating a duplicate must not rest on status: it lives in `metadata` (the create-attempt annotation) and in the reconcile's `Find` step, per [idempotency.md](idempotency.md). Status still records the identity in `signozResourceMetadata.id`, but only as a cache — if it is lost, the next reconcile re-discovers it via `Find`, so the loss forces a lookup, never a second remote object.

- **We cannot change the SigNoz API.** The identifier is assigned by the server on create and is not something the operator or the user can choose, so `signozResourceMetadata.id` is always discovered, and always empty for the interval between a CR being applied and its remote object being confirmed.

Whatever status carries therefore has to use a fixed, kind-independent condition vocabulary, has to survive being partially dropped, and has to represent "we do not yet know" as a first-class value rather than as a false negative.

## Considered options

### Flat `Ready` / `Synchronized` conditions

Two boolean conditions: `Synchronized` for "the remote matches desired" and `Ready` derived from it.

**Rejected.** It cannot tell a transient failure from a permanent one, and that distinction is the entire point of status. A `Synchronized=False` set by a 503 from an overloaded SigNoz and a `Synchronized=False` set by a spec the server will reject forever look identical, so a caller cannot know whether waiting helps, and the reconciler cannot know whether to keep retrying. The latter case (predictable failure) is a permanently-invalid resource that retries on every backoff tick indefinitely, hammering the API for a result that will never change.

### Per-kind condition types

Each kind gets its own condition types — `DashboardSynchronized`, `RuleSynchronized`.

**Rejected.** It breaks every generic consumer. `kubectl wait --for=condition=Ready` needs one type name that means the same thing on all kinds; a GitOps tool computing health across a mixed set of resources cannot special-case a growing list of per-kind strings. The information that varies per kind is *why* a resource is or is not synced, and that belongs in `reason` and `message`, which are free-form for exactly this purpose. Diverging the type as well (in addition to the per-kind detail already living in reason/message and in `Kind` itself) duplicates that information into a place tools cannot use and users cannot rely on.

### Taxonomy with uniform types

A small fixed set of condition types shared by every kind — a solvability axis (`Terminal` vs `Recoverable`), a state axis (`Synced`), and a derived summary (`Ready`) — modelled on [AWS Controllers for Kubernetes](https://github.com/aws-controllers-k8s/runtime/blob/f13ed6d/pkg/runtime/reconciler.go#L785-L792).

**Accepted.** The solvability axis is what the flat model lacks: `Terminal` is *"a stable state for a resource"* that the reconciler settles into without requeuing, so an invalid resource stops retrying instead of looping forever; `Recoverable` is the one that retries. Uniform types keep `kubectl wait --for=condition=Ready` and generic tooling working, and the per-kind detail still has a home in `reason`. Conditions are also the mechanism the Kubernetes API conventions single out — *"a standard mechanism for higher-level status reporting from a controller"* ([Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)) — so this is the idiomatic shape as well as the expressive one.

## Decision

We define one `CoreStatus`, and its `CoreSpec` counterpart, built on `metav1.Condition` and embedded inline by every kind. Both live in the versioned package alongside the kinds — `api/resources/v1alpha1/core.go`. Three choices fix that location:

- **Versioned, not shared across versions.** These types are part of a served CRD's schema (JSON tags, validation markers), and a CRD schema is per-version: Kubernetes lets a later version differ and converts between versions through a storage version rather than a shared definition — kubebuilder gives *"a separate Go package for each API version"* ([kubebuilder multi-version tutorial](https://book.kubebuilder.io/multiversion-tutorial/api-changes)). Freezing one definition across versions would defeat that, so a `v1beta1` gets its own copy plus a conversion, in `api/resources/v1beta1/`.

- **In the group, not a dedicated `core` group.** A separate, independently-versioned group of shared primitives — the ACK / `metav1` pattern — earns its keep only when many groups embed an identical type whose shape is stable enough to freeze. Only `resources` embeds these, and they will grow, so a standalone group is machinery for one consumer. If `installations` ever needs one of these types, extracting `api/core/v1alpha1/` then is mechanical and moves no kinds — nothing is lost by waiting.

- **In `resources`, not another group.** The reason is semantic: these fields describe mirroring a remote SigNoz object, which is what the `resources` group is. `installations` deploys SigNoz itself and will not embed them.

```go
type CoreStatus struct {
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // +optional
    SigNozResourceMetadata *ResourceMetadata `json:"signozResourceMetadata,omitempty"`

    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // +optional
    ObservedHash string `json:"observedHash,omitempty"`

    // +optional
    ReconciledAt metav1.Time `json:"reconciledAt,omitempty"`
}

// ResourceMetadata records the identity of the object this resource maps to in
// SigNoz. It is a nested struct rather than a bare field, so a further identifier can be added
// without a schema break.
type ResourceMetadata struct {
    // ID is the identifier SigNoz assigned, set once the operator has created
    // or adopted the remote object.
    // +optional
    ID *string `json:"id,omitempty"`
}
```

The fields:

| Field | What it holds | Why it is there |
|---|---|---|
| `conditions` | The condition set below, keyed by `type`. `+listType=map` / `+listMapKey=type` so a server-side apply merges a single condition rather than replacing the list. | The reporting channel. `metav1.Condition` + `meta.SetStatusCondition` gives correct `lastTransitionTime` handling for free. |
| `signozResourceMetadata` | A nested record of the object's identity in SigNoz; `.id` is the SigNoz-assigned identifier, set once a create or lookup confirms it, empty until then. Nested rather than a bare field so a future field slots in without a schema break. | The link from CR to remote object. An empty `.id` alongside a create-attempt annotation is the "outcome unknown" window described in [idempotency.md](idempotency.md). |
| `observedGeneration` | The `.metadata.generation` the operator last reconciled. | A reader compares it to the live `.metadata.generation` to tell whether the latest spec has been observed yet; a stale value means the operator has not caught up. Matches the Kubernetes convention that `observedGeneration` records *"the .metadata.generation that the condition was set based upon"* ([Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)). |
| `observedHash` | A hash of the last body the operator sent to SigNoz. | A fast path that lets a reconcile skip the remote GET when nothing in desired state changed. It is an optimisation, never the drift mechanism — see the consequence below. |
| `reconciledAt` | When the operator last reconciled the resource against SigNoz. | Makes the periodic re-check visible, so a reader can see the resource is being actively watched and not merely reconciled once at apply time. |

The condition vocabulary is the same on every kind. Types are fixed; kind-specific and case-specific detail lives in `reason` and `message`:

| Type | Meaning |
|---|---|
| `Terminal` | The desired state is wrong and no retry will help — a validation rejection, a name already taken, a permission denied. A stable state: the reconciler settles here and does not requeue. |
| `Recoverable` | A transient failure — 429, 5xx, a dial or connection error. The reconciler requeues at [`retryInterval`](core-spec.md) (falling back to `interval`). |
| `Synced` | Three-valued. `True` when the remote matches desired, `False` when a `Terminal` cause means it never will, and `Unknown` when the operator could not determine the answer (a transient failure, or the outcome-unknown window). |
| `Ready` | Derived, not set directly: the controller rolls up the others and reports the most serious one that applies, in the order `Terminal` > `Recoverable` > `Synced` > `Unknown` (`>` means "outranks", worst-first, so `Ready`'s reason is always the most actionable state currently true). The single condition a user or tool should wait on. |
| `Suspended` | Reconciliation is paused by [`spec.suspend`](core-spec.md); the operator is deliberately not acting on this resource. |

Which API outcome maps to `Terminal` versus `Recoverable` is a per-resource table of status codes, kept as data next to each adapter rather than as a branch inside the reconcile loop, so a new resource declares its classification instead of editing shared control flow.

## Consequences

- `kubectl wait --for=condition=Ready` works against any kind, and a GitOps tool can compute health from one condition type across a mixed set of resources. That portability is the reason the type vocabulary is fixed and per-kind detail is pushed into `reason`.

- A permanently-invalid resource settles on `Terminal` and stops retrying, instead of looping on the API forever. The cost is the mirror image: a `Terminal` resource does **not** self-heal if the cause is fixed out of band without a spec change, because a stable state is not requeued. Recovery is driven by a new `.metadata.generation` (a spec edit) or the next `interval` re-check, not by the operator noticing on its own.

- `observedHash` matching does not prove the remote is in sync — it only proves desired state has not changed since the last write. An edit made directly in the SigNoz UI leaves the hash untouched, so correctness rests on periodically fetching the remote and comparing it, with the hash only skipping that fetch when desired state is unchanged. If the hash were ever treated as the drift check, out-of-band edits would persist silently.

- `signozResourceMetadata.id` can be empty on a resource that has in fact been created, during the window before the outcome is confirmed. That window is reported as `Synced=Unknown`, and the create-attempt annotation in `metadata` — not status — is what prevents a duplicate. A backup that restores spec and metadata but drops status loses `signozResourceMetadata.id` and forces a lookup on the next reconcile, which is acceptable precisely because identity does not depend on status surviving.

- `lastTransitionTime` moves only on a real state change, because conditions are written through `meta.SetStatusCondition`. A reconcile that observes no change writes no transition, so the object does not churn and `resourceVersion` does not advance needlessly.

- Every adapter must supply its own error-code classification table. An adapter that leaves it empty gets no `Terminal` states, so its invalid resources fall back to `Recoverable` and retry forever — the failure the taxonomy exists to prevent — which makes the table a required part of adding a resource, not an optional refinement.

## Sources

- [Kubernetes API conventions — typical status properties](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)
- [Kubebuilder book — multi-version tutorial](https://book.kubebuilder.io/multiversion-tutorial/api-changes)
