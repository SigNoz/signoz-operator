# Idempotency

> Create resources at most once, and say so when unsure

## Context

Every resource this operator manages is a record in SigNoz, created over HTTP. Creation is a `POST` and SigNoz assigns the identifier, so the operator has to record afterwards which remote object a given custom resource corresponds to. That recording cannot be atomic with the creation itself, which means this sequence is always reachable:

```
POST → 201 (object created) → response lost → identifier never recorded
```

No client-side mechanism prevents that object existing. RFC 9110 states the limit, and states it about *idempotent* methods, which carry the stronger guarantee: *"A client cannot know whether an idempotent request succeeded if a communication failure occurs before receiving a response"* ([RFC 9110 §9.2.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.2)). The same section is why HTTP distinguishes idempotent methods at all — it permits automatic retry only for them, and `POST` is not among them.

So "create each object exactly once" is not an achievable goal, and any design aiming at it is solving the wrong problem. What is achievable is never making the situation worse, and always being able to say what is uncertain.

## Constraints

Three constraints bound the design. Two are facts about the API that we cannot change; the third is a choice, and one we intend to hold.

- **The SigNoz API cannot be changed.** Whether it accepts a user-supplied identifier on create is unverified and the design does not depend on the answer. What is certain is that there is no idempotent `PUT`-to-create and no idempotency-key header. SigNoz does enforce uniqueness for most resources, returning 409 on a colliding create, and that is worth using wherever it holds — but it does not hold everywhere. Where there is no uniqueness constraint a duplicate create simply succeeds and yields a second object, so identity for those resources is something the operator has to establish itself.

- **We must not write an ownership marker into a SigNoz object.** Tags, labels and annotations belong to the user. Putting our own identifier in them adds data the user never asked for, and collides with values they set deliberately. Each of those fields also already carries a meaning, and re-purposing one for bookkeeping breaks that meaning in ways particular to each resource — on `AlertRule` the obvious field is `labels`, but rule labels are the alerting routing key and feed route-policy expression matching, so a marker there would change which notifications fire. Other resources have their own such hazards, and some expose no suitable field at all. This is a decision rather than a limitation: even if every resource had a spare field, we would not use one, because the approach does not generalise and each new resource would need its own judgement about where a marker could safely go.

- **`metadata.name` is not universally the key.** Some resources are identified by an email address, which does not satisfy the validation rules for a Kubernetes object name; others are identified by a combination of fields rather than by any single one.

Whatever we build therefore has to be a single mechanism that behaves the same way on every surface it is applied to.

## Considered options

### Ownership marker written into the remote object

Stamp the custom resource's UID into a tag, label or annotation on the SigNoz object, then search for it during recovery.

**Rejected.** It is superficially the most attractive option, because the marker is written in the create body and is therefore atomic with creation, which makes recovery a search for our own UID. It is ruled out by the constraint above: the fields it would use belong to the user, and writing operator bookkeeping into them is a re-purposing we are not willing to do. Coverage is a secondary problem — some resources expose no such field at all — but coverage is not the reason. Even universal availability would not make it acceptable.

### Rely on server-enforced uniqueness alone

Treat a 409 on create as proof the object already exists, and resolve it from there.

**Accepted, in part.** Where SigNoz enforces uniqueness this is the strongest signal available: the collision is positive proof the earlier create succeeded, it costs nothing, and it needs no state on our side. It cannot stand alone, because it is silent exactly where it is most needed — for resources with no uniqueness constraint a duplicate create returns success and a second object, and those are the cases where a duplicate does the most damage.

### Idempotency keys

Send a client-generated key per logical create and let the server deduplicate.

**Rejected.** This is the correct solution to the general problem — the IETF draft exists precisely to *"make non-idempotent HTTP methods such as POST or PATCH fault-tolerant"* ([draft-ietf-httpapi-idempotency-key-header-07](https://datatracker.ietf.org/doc/html/draft-ietf-httpapi-idempotency-key-header-07)), and Google's `request_id` equivalent specifies that on a duplicate the server *"should return the response for the previously successful request, because the client most likely did not receive the previous response"* ([AIP-155](https://google.aip.dev/155)). It requires server support we cannot add.

### Write-ahead create record plus identity resolution

Record the intent to create on the custom resource before calling, and resolve the outcome afterwards by an identity derived from desired state.

**Accepted.** It needs nothing from the server, so it behaves identically on every surface, which is the property the constraints demand. It is also the established fallback for this situation. Crossplane records the same intent in three annotations — `crossplane.io/external-create-pending`, described as *"The last time the provider was about to create the resource"*, alongside `external-create-succeeded` and `external-create-failed` ([Crossplane managed resources](https://docs.crossplane.io/latest/managed-resources/managed-resources/)) — and when pending is newer than both others it stops rather than retrying, emitting *"cannot determine creation result - remove the crossplane.io/external-create-pending annotation if it is safe to proceed"* ([Crossplane managed resources](https://docs.crossplane.io/latest/managed-resources/managed-resources/)).

## Decision

We combine the two accepted options: the operator writes a `resources.signoz.io/create-attempt` annotation onto the custom resource before issuing the `POST`, never retries a create whose outcome is unknown, and resolves the outcome through a `Find` built on a per-resource identity function. Server-enforced uniqueness is folded into that path rather than replacing it — where a 409 comes back it is treated as proof and short-circuits to resolution, and where it does not, the write-ahead record carries the same guarantee unaided.

The guarantee is deliberately narrower than exactly-once:

> The operator never issues a create whose outcome it might not learn, and never issues a second create while a previous outcome is unknown.

Only two things vary per resource: an `Identity` function returning a key derived purely from desired state, and a `List` or `Get` using the cheapest filter the endpoint offers. `Find` composes them generically — try the recorded identifier, fall through on 404, then resolve by identity, where exactly one match is ours, zero means not created, and more than one is ambiguous and never guessed.

The create sequence is then identical everywhere. `A` is the annotation:

```
1. desired, hash := Render(cr)

2. if status.signozID == "" and A is absent:
       Find()
         found      → adopt (gated on the adopt-existing annotation)
         ambiguous  → Terminal
         not found  → continue

3. patch A onto the CR                       ← durable, and BEFORE the call
   (if this write fails, do not POST)

4. POST — never retried
     201             → status.signozID = id; clear A; Synced=True
     409             → it exists: Find() → adopt; if still not found → Terminal
     other 4xx       → Terminal; clear A (nothing was created)
     5xx / timeout / connection error
                     → outcome UNKNOWN: leave A set; Synced=Unknown; requeue

5. any reconcile with A set and status.signozID == "":
       Find()
         1 match              → adopt; clear A
         >1                   → Terminal (ambiguous) — never guess
         0, within grace      → Synced=Unknown; requeue
         0, past grace period → nothing was created; clear A; go to 3
```

The annotation lives in `metadata` rather than `status` because status is a subresource and there are real paths where the object survives without it, such as a restore from a backup that excludes status.

The grace period exists because an immediate `Find` returning zero does not prove the create failed — the write may not be readable yet. Crossplane separates this from cache staleness as its own problem, citing external APIs where *"a newly created external resource may take some time before our ExternalClient's observe call can confirm it exists"*, and adopts the same remedy: *"During this grace period we'll requeue and keep waiting if Observe determines that the external resource doesn't exist, rather than (re)creating it"* ([crossplane-runtime#283](https://github.com/crossplane/crossplane-runtime/pull/283)). Default 30 seconds, tunable by flag.

## Consequences

- A duplicate is never created silently. The worst case is an object in SigNoz that the operator cannot name, reported as `Synced=Unknown` with the create-attempt timestamp, which is the information needed to find it by hand.

- **The HTTP client must not automatically retry `POST`.** This is the most likely source of duplicates in practice and needs no crash: a create that times out client-side but succeeded server-side, retried, is a duplicate. Retries are for `GET`, `PUT` and `DELETE` only.

- **409 means two different things depending on method.** On create it is "already exists" and the response is to resolve and adopt. On delete it is "still referenced by some resource", which is retryable and not an adoption signal. Classification is by method, not by status code alone.

- Adoption is never implicit. A custom resource whose identity matches an object already in SigNoz goes `Terminal` unless the adopt-existing annotation is present. Importing an existing object costs one extra step; the alternative is overwriting something a user built by hand.

- Ambiguity is reported, not resolved. More than one candidate produces `Terminal` — for example *"ambiguous: 2 alert rules named X"* — and the operator does not write, delete, or guess. This is reachable only for resources whose uniqueness the operator enforces itself, and in practice only when someone creates a same-named object through the SigNoz UI.

- `Find` costs a list call, so it runs only on the recovery path when the identifier is empty, never on every reconcile.

- Every adapter must supply `Identity` and a lookup, and `Identity` must never derive from a server response. An implementation that reads back a server-assigned field and calls it identity reintroduces the original problem.

## Sources

- [RFC 9110 §9.2.2 — Idempotent Methods](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.2)
- [draft-ietf-httpapi-idempotency-key-header-07 — The Idempotency-Key HTTP Header Field](https://datatracker.ietf.org/doc/html/draft-ietf-httpapi-idempotency-key-header-07)
- [Google AIP-155 — Request identification](https://google.aip.dev/155)
- [Crossplane — Managed resources](https://docs.crossplane.io/latest/managed-resources/managed-resources/)
- [crossplane-runtime#283 — Account for two different kinds of consistency issues](https://github.com/crossplane/crossplane-runtime/pull/283)
