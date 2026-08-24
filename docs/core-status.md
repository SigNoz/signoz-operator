# Core status

> Report what we observed, distinguish a transient failure from a permanent one, and expose one condition worth waiting on.

## Context

The operator keeps reconciling towards a desired state (in SigNoz) held in a custom resource (in K8s). `.status` is the operator's report back: what it observed about the remote object, whether the two are in sync, and — when they are not — why. It is the reporting channel a user or a GitOps tool reads, because the operator never writes anything back into `.spec` and never annotates the remote object. Running `kubectl get` or `kubectl wait` against the CR is the whole interface.

Status is written under a status subresource (`+kubebuilder:subresource:status`), so a spec edit and a status update are separate writes and cannot race each other. A reader needs three things from it: did the last reconcile succeed, is the resource in a state that will change on its own or one that is stuck until a human acts, and which remote object does this CR correspond to. A flat "ready / not ready" answers the first and none of the rest, and in particular cannot tell a caller whether waiting will help.

The remote object is created over HTTP with a server-assigned identifier, so the identifier is observed state discovered after the create, not desired state the user supplies — see [idempotency.md](idempotency.md). Status therefore has to carry that identifier once it is known, and has to be honest about the window in which it is not yet known.

The desired-state half of the same shared struct — the provider-config reference, reconcile interval, suspend switch, and reclaim policy — is [core-spec.md](core-spec.md); this document covers only the observed-state half.

## Constraints

- **Condition *types* must be uniform across every kind.** This is a choice, not a platform limit. `kubectl wait --for=condition=Ready`, GitOps health checks, and every generic dashboard key off a condition type string; if `Dashboard` reports `DashboardSynchronized` and `Rule` reports `RuleSynchronized`, none of those tools work across kinds. Per-kind detail **must not** go in the type — it goes in the `reason` and `message`. The type vocabulary is fixed and shared.

- **Status must not be load-bearing for identity.** Status is a subresource and there are real paths where the object survives without it — a restore from a backup that excludes status is the common one. So the *guarantee* against creating a duplicate must not rest on status: it lives in `metadata` (the create-attempt annotation) and in the reconcile's `Find` step, per [idempotency.md](idempotency.md). Status still records the identity in `signozResource.id`, but only as a cache — if it is lost, the next reconcile re-discovers it via `Find`, so the loss forces a lookup, never a second remote object.

- **We cannot change the SigNoz API.** The identifier is assigned by the server on create and is not something the operator or the user can choose, so `signozResource.id` is always discovered, and always empty for the interval between a CR being applied and its remote object being confirmed.

Whatever status carries therefore has to use a fixed, kind-independent condition vocabulary, has to survive being partially dropped, and has to represent "we do not yet know" as a first-class value rather than as a false negative.

## Considered options

### Flat `Ready` / `Synchronized` conditions

Two boolean conditions: `Synchronized` for "the remote matches desired" and `Ready` derived from it.

**Rejected.** It cannot tell a transient failure from a permanent one, and that distinction is the entire point of status. A `Synchronized=False` set by a 503 from an overloaded SigNoz and a `Synchronized=False` set by a spec the server will reject forever look identical, so a caller cannot know whether waiting helps, and the reconciler cannot know whether to keep retrying. The latter case (predictable failure) is a permanently-invalid resource that retries on every backoff tick indefinitely, hammering the API for a result that will never change.

### Per-kind condition types

Each kind gets its own condition types — `DashboardSynchronized`, `RuleSynchronized`.

**Rejected.** It breaks every generic consumer. `kubectl wait --for=condition=Ready` needs one type name that means the same thing on all kinds; a GitOps tool computing health across a mixed set of resources cannot special-case a growing list of per-kind strings. The information that varies per kind is *why* a resource is or is not synced, and that belongs in `reason` and `message`, which are free-form for exactly this purpose. Diverging the type as well (in addition to the per-kind detail already living in reason/message and in `Kind` itself) duplicates that information into a place tools cannot use and users cannot rely on.

### Taxonomy with uniform types

A small fixed set of condition types shared by every kind: a solvability axis (`Terminal` vs `Recoverable`), a state axis (`Synced`), and a derived summary (`Ready`).

**Accepted.** The solvability axis is what the flat model lacks, and it is the axis a reconciler needs in order to stop. `Terminal` is a stable state — the reconciler settles there without requeuing, so an invalid resource stops retrying; `Recoverable` is the one that retries. Two failures that a flat model renders identically are then distinguishable by a caller and by the loop itself. Conditions are also the mechanism the Kubernetes API conventions single out — *"Conditions provide a standard mechanism for higher-level status reporting from a controller."* ([Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)) — so this is the idiomatic shape as well as the expressive one, and uniform types keep `kubectl wait --for=condition=Ready` and generic tooling working while the per-kind detail keeps its home in `reason`.

## Decision

We define one `CoreStatus`, and its `CoreSpec` counterpart, built on `metav1.Condition` and embedded inline by every kind. Both live in the versioned package alongside the kinds — `api/resources/v1alpha1/core.go`. Three choices fix that shape and location:

- **Embedded inline.** Every kind embeds the struct with `json:",inline"`, so the shared fields sit at the same path on every kind and can be read by one jsonpath, one CEL rule, and one line of shared Go. Nesting them under a key would make that path per-kind, which is the arrangement [resources.md](resources.md) rejects for the payload as well.

- **Versioned.** These types are part of a served CRD's schema (JSON tags, validation markers), and a CRD schema is per-version: a later version is free to change shape, and Kubernetes reconciles the difference by conversion — *"all versions must be safely round-tripable through each other"* ([kubebuilder multi-version tutorial](https://book.kubebuilder.io/multiversion-tutorial/api-changes)), wired through a designated hub version ([hubs and spokes](https://book.kubebuilder.io/multiversion-tutorial/conversion-concepts)). A `v1beta1` therefore gets its own copy of these types plus a conversion, in `api/resources/v1beta1/`.

- **In the `resources` group.** These fields describe mirroring a remote SigNoz object, which is what the `resources` group is; `installations` deploys SigNoz itself and does not embed them. An independently-versioned group of shared primitives earns its keep when many groups embed an identical type whose shape is stable enough to freeze, and one consumer with a shape still growing is not that. If `installations` ever needs one of these types, extracting `api/core/v1alpha1/` at that point is mechanical and moves no kinds.

```go
type CoreStatus struct {
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // +optional
    SigNozResource *SigNozResource `json:"signozResource,omitempty"`

    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // +optional
    ObservedHash string `json:"observedHash,omitempty"`

    // +optional
    ReconciledAt metav1.Time `json:"reconciledAt,omitempty"`
}

// SigNozResource records the identity of the object this resource maps to in
// SigNoz. It is a nested struct rather than a bare field, so a further identifier can be added
// without a schema break.
type SigNozResource struct {
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
| `signozResource` | A nested record of the object's identity in SigNoz; `.id` is the SigNoz-assigned identifier, set once a create or lookup confirms it, empty until then. Nested rather than a bare field so a future field slots in without a schema break. | The link from CR to remote object. An empty `.id` alongside a create-attempt annotation is the "outcome unknown" window described in [idempotency.md](idempotency.md). |
| `observedGeneration` | The `.metadata.generation` the operator last reconciled. | A reader compares it to the live `.metadata.generation` to tell whether the latest spec has been observed yet; a stale value means the operator has not caught up. Matches the Kubernetes convention that *"observedGeneration represents the .metadata.generation that the condition was set based upon."* ([Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)) |
| `observedHash` | A canonical hash of the body last written to SigNoz — canonical so that reformatting, or moving identical content between the template forms, does not change it ([resources.md](resources.md)). | Detects a spec edit without consulting the remote: when the hash of the body about to be sent differs from the hash last written, the reconcile updates SigNoz even if the field-by-field compare of the fetched remote reports no difference. That is the only detector for an edit the compare cannot see — a field the read response does not echo back, or a gap in a kind's compare mapping. What the hash itself cannot see is in the consequence below. |
| `reconciledAt` | When the operator last reconciled the resource against SigNoz. | Makes the periodic re-check visible, so a reader can see the resource is being actively watched and not merely reconciled once at apply time. |

The condition vocabulary is the same on every kind. Types are fixed; kind-specific and case-specific detail lives in `reason` and `message`:

| Type | Meaning |
|---|---|
| `Terminal` | The desired state is wrong and no retry will help — a validation rejection, a name already taken, a credential the server rejects. A stable state: the reconciler settles here and does not requeue, on `interval` or otherwise. |
| `Recoverable` | A transient failure — 429, 5xx, a dial or connection error. The reconciler requeues at [`retryInterval`](core-spec.md) (falling back to `interval`). |
| `Synced` | Three-valued. `True` when the remote matches desired, `False` when a `Terminal` cause means it never will, and `Unknown` when the operator could not determine the answer (a transient failure, or the outcome-unknown window). |
| `Ready` | Derived, not set directly: the controller rolls up the others and reports the most serious that applies, in the precedence `Suspended`, then `Terminal`, then `Recoverable`, then `Synced` — so `Ready`'s reason is always the most actionable state currently true. Its status follows the condition it summarises, including `Unknown`. The single condition a user or tool should wait on. |
| `Suspended` | Reconciliation is paused by [`spec.suspend`](core-spec.md); the operator is deliberately not acting on this resource. |

Which HTTP status maps to `Terminal` versus `Recoverable` is fixed, the same for every kind: 400, 401, 403 and 409 are `Terminal` — a rejected body, a rejected credential and a collision are failures no retry will fix — and any other non-2xx status is `Recoverable`, so a status the classifier does not recognise keeps retrying. Method-specific meaning stays in the reconcile flow, not in the classifier: a 409 on create is intercepted and resolved by lookup before it can settle ([idempotency.md](idempotency.md)), and a failed delete is retried whatever its classification, so reclaim never strands on a settled state.

`reason` carries one further distinction that no condition type does: whether the failure is attributable to the resource's own body or to the backend it writes through. A body the server rejected, a colliding name and an unparseable payload are the resource's; an unresolvable `providerConfigRef`, a missing Secret key, a rejected credential and an unreachable endpoint belong to the referenced `ProviderConfig`, and their reasons name it. That is what lets a user tell "my dashboard is wrong" from "my credential is wrong" without reading every other resource on the cluster — see [provider-config.md](provider-config.md).

## Consequences

- `kubectl wait --for=condition=Ready` works against any kind, and a GitOps tool can compute health from one condition type across a mixed set of resources. That portability is the reason the type vocabulary is fixed and per-kind detail is pushed into `reason`.

- A permanently-invalid resource settles on `Terminal` and stops retrying, instead of looping on the API forever. The cost is the mirror image: a `Terminal` resource is not requeued at all, so it does not self-heal when the cause is fixed somewhere the operator is not watching. Three things bring it back — a new `.metadata.generation`, a change to the `ProviderConfig` it names or to a Secret that config reads, and an operator restart. A remote-side fix, such as freeing the name that collided, is not among them.

- A suspended resource reports `Ready` with the `Suspended` reason, so a health rollup counts it as not ready. That is the intended reading — nothing is driving the resource — but it means pausing one resource degrades the health of any set it belongs to, and a `kubectl wait` on it blocks until it is resumed.

- `observedHash` and the remote compare cover different failures, and neither subsumes the other. The hash is computed entirely from local state, so a match proves only that desired state has not changed since the last write — an edit made directly in the SigNoz UI leaves it untouched, and only the fetch-and-compare that every reconcile performs catches that. The compare in turn sees only the fields the read response carries, so a spec edit to a field the server stores but does not return is invisible to it — the hash is what catches that edit and forces the write. An out-of-band edit to such a field is caught by neither: the resource reports `Synced` while that field has drifted, and the drift persists until the next write, whose payload is rendered from the spec alone and therefore restores every field it carries. A kind that cannot tolerate that window needs the server to expose the field, or a revision of it, in reads.

- `signozResource.id` can be empty on a resource that has in fact been created, during the window before the outcome is confirmed. That window is reported as `Synced=Unknown`, and the create-attempt annotation in `metadata` — not status — is what prevents a duplicate. A backup that restores spec and metadata but drops status loses `signozResource.id` and forces a lookup on the next reconcile, which is acceptable precisely because identity does not depend on status surviving.

- `lastTransitionTime` moves only on a real state change, because conditions are written through `meta.SetStatusCondition`. A reconcile that observes no change writes no transition, so the object does not churn and `resourceVersion` does not advance needlessly.

- Classification is shared policy, so adding a resource cannot get it wrong: every kind settles on `Terminal` for the statuses no retry will fix, and none can retry an invalid body forever through a classification gap. The cost is uniformity — an endpoint whose semantics genuinely diverge, a status transient on one API and permanent on another, needs the shared policy revisited rather than a local exception.

## Sources

- [Kubernetes API conventions — typical status properties](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)
- [Kubebuilder book — multi-version tutorial: changing things up](https://book.kubebuilder.io/multiversion-tutorial/api-changes)
- [Kubebuilder book — hubs, spokes, and other wheel metaphors](https://book.kubebuilder.io/multiversion-tutorial/conversion-concepts)
