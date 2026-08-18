# Resources

> The shared controls sit at the root of every kind's spec; the SigNoz object sits under `objectTemplate`, as typed fields or as its own JSON.

## Context

[`CoreSpec`](core-spec.md) holds the controls every kind shares — which backend to write through, the reconcile cadence, the suspend switch, the reclaim policy — and [`CoreStatus`](core-status.md) holds the observed-state half. What remains for each kind is its content: the fields that describe the SigNoz object itself, a dashboard's panels or an alert rule's condition. Where those fields sit relative to the shared controls is the modelling decision, and it settles three things at once — whether a SigNoz field can shadow one of our controls, whether shared reconcile machinery can obtain a kind's desired body without knowing the kind, and how much of the published API has to change when either side moves.

Two facts about the SigNoz side set the terms. The body of a SigNoz object is a structured document with its own internal shape: generally a `kind`/`spec` model ([domain_config.go:61-64](https://github.com/SigNoz/signoz/blob/c40ebb027b2bac8b7fa5d219c7a16425adfe64da/pkg/types/authtypes/domain_config.go#L61-L64)), or a `schemaVersion`/`spec` model, or both. And that shape moves on SigNoz's release cadence: an untyped resource becomes a typed one, and a payload schema that has already been replaced once can be replaced again.

SigNoz also already draws the line between the part of an object a user authors and the part the server owns. For most resources, fields such as `kind`, `name` and `spec` are user-authored while `id` and `createdAt` are server-owned. The authored part is what a custom resource has to carry; the server-owned part is what status records.

The two halves of that sentence describe two different users. Someone declaring an auth domain writes a dozen fields by hand and wants them checked at `kubectl apply`. Someone moving a dashboard between workspaces has a JSON document produced by the download control and wants to paste it, unmodified, without waiting for us to model whatever SigNoz shipped last week. A resource has to serve both without becoming two APIs.

## Constraints

- **The names inside a SigNoz object body are not ours.** Fields are added and renamed on SigNoz's cadence, and a whole payload schema has already been replaced once. Any level of our API that mixes those names with ours is a namespace we only half control.
- **The shared controls must be at the same path on every kind.** This is a choice. `kubectl get -o jsonpath`, admission policies, and GitOps tooling key on a literal path, and so does our own reconciler; a control that lives at a different depth per kind cannot be read generically.
- **Shared reconcile machinery must obtain a kind's desired body without knowing the kind.** The reconcile writes that body to SigNoz and records a hash of it in [`observedHash`](core-status.md). Both must go through one seam, so that adding a kind means declaring a payload type rather than editing shared control flow.
- **A new SigNoz field must be usable before we model it.** SigNoz adding a field must not require a CRD version bump, and must not require an operator release before anyone can set it.
- **A body the schema does model must be validated by the API server, not by the reconciler.** A typo in an authored resource must fail at apply. Discovering it in a status condition minutes later is a worse failure, and it is the failure [core-spec.md](core-spec.md) rejects annotations for.

So the resource-specific fields must live at a single fixed path that is identical on every kind, must carry the SigNoz body's own shape, must not be able to shadow a shared control, and must admit both an authored form the API server checks and a document it does not.

## Considered options

### Resource-specific fields at the root of the spec, beside the shared controls

Embed `CoreSpec` inline and declare each kind's own fields as siblings, so a resource's `tags` sits next to `providerConfigRef`.

**Rejected.** It puts two sets of names in one namespace when only one set is ours, and the collision is not hypothetical. A SigNoz resource can carry a field called `interval`, which is also what `CoreSpec` calls the reconcile cadence; the same is true of any other plain noun a shared control spends. Whichever side yields, the result is a name bent out of shape to avoid the other — and since the body's names are not ours to bend, it is the operator's control that ends up qualified. The semantic clash arrives before the literal one: a resource's `tags` at the spec root reads as Kubernetes-side grouping when it is SigNoz-side grouping, next to a `metadata.labels` that means something else.

It is genuinely tempting — the flattest YAML, no extra indentation — and it is what generated APIs tend to produce, since a spec derived from a vendor's API model naturally lands at the spec root. The lesson from the operators that went that way is what happens next: with the payload owning the root, the operator's own controls have nowhere to go. They migrate into annotations, which [core-spec.md](core-spec.md) rules out for anything our controller consumes ([ACK — deletion policy](https://aws-controllers-k8s.github.io/community/docs/user-docs/deletion-policy/)), or the first one that must live in the spec arrives as a breaking change and a new API version, because the fence that should have been there from the start has to be introduced around an already-published payload ([perses-operator#128](https://github.com/perses/perses-operator/pull/128), [v1alpha1/persesdashboard_types.go:36](https://github.com/perses/perses-operator/blob/510f43407a467af4385ba794d721143ecbdc73bf/api/v1alpha1/persesdashboard_types.go#L36) and [v1alpha2/persesdashboard_types.go:33-43](https://github.com/perses/perses-operator/blob/510f43407a467af4385ba794d721143ecbdc73bf/api/v1alpha2/persesdashboard_types.go#L33-L43)). Our equivalent field, `providerConfigRef`, is required on day one, so a flat spec would start us where that migration ends.

### Resource-specific fields under a key named for the kind

Fence each kind's payload under its own name — `spec.dashboard`, `spec.authDomain`.

**Rejected.** It is the strongest of the rejected options: naming the key after the remote type is a well-attested convention among operators that mirror exactly one remote object per kind ([Keycloak — realm import](https://www.keycloak.org/operator/realm-import)). Go code stays kind-independent either way, through an accessor. What a varying key costs is everything that addresses the payload by its literal path: one CEL rule cannot be written for all kinds, printer columns and `kubectl -o jsonpath` need a per-kind path, and a reader learning the second kind cannot reuse what they learned from the first. A fixed key buys one shape to learn and one path to write against, and the key restates nothing that `kind:` has already said.

### Shared controls under a key, resource-specific fields at the root

Invert the nesting: group the shared controls under one key and let each kind's own fields own the spec root. Operators do ship this, keeping the remote object's fields flat in the spec and fencing the operator's own behaviour behind a single key that the doc marks as interpreted locally and never sent upstream ([ASO — resources API](https://azure.github.io/azure-service-operator/reference/resources/v1api20200601/), [KEDA — ScaledObject specification](https://keda.sh/docs/latest/reference/scaledobject-spec/)).

**Rejected.** Whichever half sits at the root, the fence key itself is a name at that root, and this arrangement spends that name in the half we do not control: the chosen word becomes one SigNoz must never use in any resource body, in any future schema version, for every kind we ever add. It also makes the root of the spec a different shape on every kind while the fields tooling reads most often move one level deeper. The instinct behind it is sound — the payload is the bulky, interesting part and reads well unindented — but it buys that readability by betting a reserved word against an API we do not own.

### One kind, a type discriminator, and an untyped map

A single kind whose spec carries a `type` and a string map of that type's fields.

**Rejected.** A string map sits outside the structural schema, so every field inside it is a string the API server does not validate, default, or publish. The cost is visible in the manifests before it is visible anywhere else: a count is written `'5'` and a boolean `'false'`, because both have to be quoted to survive a `map[string]string` ([KEDA — Apache Kafka scaler](https://keda.sh/docs/latest/scalers/apache-kafka/)). The deeper cost is that validation, defaulting, enums, optionality and deprecation do not disappear when they leave the schema — they get rebuilt inside the controller, as a private tag language and a validator interface that together reimplement what kubebuilder markers would have had the API server enforce at apply time ([typed_config.go:43](https://github.com/kedacore/keda/blob/aeb7920f97cd9ca027fbd4c4942f70143974c893/pkg/scalers/scalersconfig/typed_config.go#L43), [typed_config.go:64-86](https://github.com/kedacore/keda/blob/aeb7920f97cd9ca027fbd4c4942f70143974c893/pkg/scalers/scalersconfig/typed_config.go#L64-L86)).

That trade pays when the payload set cannot be enumerated — a catalogue that grows continuously, where a map means a new entry costs no schema change. Ours is a set we can enumerate and whose fields we hold as Go structs, so we would be paying the price without buying anything.

### A separate kind per form

Ship a typed kind and an opaque kind for each resource, and let a user pick between them at the `kind:` line.

**Rejected.** The objection is to where the choice is expressed, not to which form is better. Splitting by kind doubles the CRDs, the RBAC rules and the reference docs, and it makes changing form a delete-and-recreate across two kinds: the old object's deletion runs the finalizer, which under a `Delete` reclaim policy takes the SigNoz object with it. A choice made inside one kind is an in-place edit.

### One fixed key holding the object in interchangeable forms

Embed `CoreSpec` inline at the spec root and put the SigNoz object under one key with the same name on every kind, that key admitting the object either as typed fields or as its own JSON.

**Accepted.** The one shared name is in our namespace, where we control every other name, so no future SigNoz field can reach it. The path is literal and kind-independent, so one seam serves hashing, diffing and the write path. And because the forms sit inside the fence rather than at the spec root, adding one later — compressed JSON, a ConfigMap reference — changes no other field and no validation rule.

## Decision

Every kind's spec is `CoreSpec` inline plus one required field, `objectTemplate`, which carries the SigNoz object in exactly one form: `spec` for typed fields or `jsonSpec` for the request body verbatim, with `gzipJsonSpec` reserved for that body compressed. Every kind's status is `CoreStatus` inline.

```go
// DashboardSpec is the desired state of a Dashboard.
type DashboardSpec struct {
    CoreSpec `json:",inline"`

    // ObjectTemplate is the SigNoz dashboard to apply, in exactly one of the
    // forms below.
    // +kubebuilder:validation:Required
    ObjectTemplate DashboardObjectTemplate `json:"objectTemplate"`
}

// DashboardObjectTemplate carries one dashboard in exactly one form. Every
// member is a form of the same body; nothing else belongs in this struct.
//
// Reserved for a later version, and covered by the same property count:
//
//	GzipJSONSpec []byte `json:"gzipJsonSpec,omitempty"`
//
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type DashboardObjectTemplate struct {
    // Spec is the dashboard as typed fields, validated by the API server
    // against this CRD's schema.
    // +optional
    Spec *DashboardConfig `json:"spec,omitempty"`

    // JSONSpec is the specification of the SigNoz object, sent verbatim.
    // +optional
    // +kubebuilder:validation:MinLength=1
    JSONSpec *string `json:"jsonSpec,omitempty"`
}

// DashboardStatus is the observed state of a Dashboard.
type DashboardStatus struct {
    CoreStatus `json:",inline"`
}
```

```yaml
apiVersion: resources.signoz.io/v1alpha1
kind: Dashboard
metadata:
  name: api-latency
spec:
  providerConfigRef:
    name: prod
  interval: 5m
  objectTemplate:
    spec:
      name: api-latency
      tags:
        - key: team
          value: platform
      spec:
        display:
          name: API latency
        panels: {}
        layouts: []
        variables: []
```

```yaml
apiVersion: resources.signoz.io/v1alpha1
kind: Dashboard
metadata:
  name: api-latency
spec:
  providerConfigRef:
    name: prod
  objectTemplate:
    jsonSpec: |-
      {
        "schemaVersion": "v6",
        "name": "api-latency",
        "tags": [{"key": "team", "value": "platform"}],
        "spec": {"display": {"name": "API latency"}, "panels": {}, "layouts": [], "variables": []}
      }
```

### The key is named `objectTemplate`

Kubernetes has one consistent convention for a field that carries a description of an object the controller will bring into existence, and it is `<noun>Template` holding a trimmed `metadata` beside a verbatim `spec`. `CronJob.spec.jobTemplate` is a `JobTemplateSpec` of *"metadata"* and *"Specification of the desired behavior of the job"* ([CronJob API](https://kubernetes.io/docs/reference/kubernetes-api/batch/cron-job-v1/)); `PodTemplateSpec` *"describes the data a pod should have when created from a template"* ([PodTemplate API](https://kubernetes.io/docs/reference/kubernetes-api/core/pod-template-v1/)); a pod's `ephemeral.volumeClaimTemplate` carries `metadata` and `spec` for the claim to create ([Ephemeral volumes](https://kubernetes.io/docs/concepts/storage/ephemeral-volumes/)). `ResourceClaimTemplateSpec` states the contract most explicitly: its `metadata` *"may contain labels and annotations that will be copied into the ResourceClaim when creating it. No other fields are allowed and will be rejected during validation"*, and its `spec` is such that *"The entire content is copied unchanged into the ResourceClaim that gets created from this template"* ([ResourceClaimTemplate API](https://kubernetes.io/docs/reference/kubernetes-api/resource/resource-claim-template-v1/)). Copied unchanged into the object being created is what this field does.

The `object` prefix is load-bearing. Bare `template` on a custom resource reads as a pod template, because that is what the bare word introduces wherever it appears in the workload APIs. `objectTemplate` says which object is being described without borrowing a word a SigNoz body might one day use.

The convention also fixes the name of the typed form. Its child is `spec`, so a dashboard's panels are addressed at `spec.objectTemplate.spec.spec.panels` — the outer `spec` is the fence's typed form, the inner one is the dashboard body's own. Where we depart from the convention is the sibling: a template's other child is `metadata`, and a SigNoz object has no Kubernetes labels or annotations to copy, so that slot is spent on the other forms of the same body instead.

Names ruled out:

| Name | Why not |
|---|---|
| `config` | The most-used name for a fenced foreign payload, in Kubernetes ([DeviceClass API](https://kubernetes.io/docs/reference/kubernetes-api/resource/device-class-v1/)) and among operators ([OpenTelemetry Operator](https://opentelemetry.io/docs/platforms/kubernetes/operator/)) — and it collides on the first kind drafted. A SigNoz auth domain's body has a `config` of its own ([domain.go:43-48](https://github.com/SigNoz/signoz/blob/c40ebb027b2bac8b7fa5d219c7a16425adfe64da/pkg/types/authtypes/domain.go#L43-L48)), so the path would read `spec.config.spec.config.kind`. It also sits one line under `providerConfigRef`, which names a `ProviderConfig`. |
| `spec` | The path to a dashboard's panels would be four `spec` segments deep. Server-side apply, CEL and JSON patch all handle it, but the word stops selecting anything: an API server error at `spec.spec.spec.spec.panels[2]` gives a reader four identical segments, only one of which is ours. |
| `parameters` | Kubernetes attaches this name to payloads it does not validate — `StorageClass.parameters` ([StorageClass API](https://kubernetes.io/docs/reference/kubernetes-api/storage/storage-class-v1/)), `DeviceClass`'s `opaque.parameters`. The typed form is checked by the API server, so the name would advertise the wrong contract. |
| `definition` | Carries no competing meaning, and no precedent we found: as an envelope for a remote object's fields it appears in none of the operators surveyed. |
| `body`, `payload` | Name the transport. A declarative resource should not read as an HTTP client. |

Status takes no name from the fence, because it holds no template and no copy of the remote object. It reports what the operator observed: the conditions, the generation and hash it acted on, and the SigNoz identifier it needs to find the object again — which [core-status.md](core-status.md) already qualifies as a cache rather than a source of truth. A reader who wants the remote object reads SigNoz, which is authoritative and current where a mirror is neither.

Mirroring is tempting for a value the server computes that a user then has to act on, and an auth domain's SAML relay-state path looks like that case. It is not one: SigNoz builds that path from a constant and the domain identifier ([authn.go:57-71](https://github.com/SigNoz/signoz/blob/c40ebb027b2bac8b7fa5d219c7a16425adfe64da/pkg/types/authtypes/authn.go#L57-L71)), so it is derivable from the identifier status already carries. The general form of that is the reason to decline the whole class: a mirrored field is stale at `interval` granularity while reading as current, it would have us model the response shape in a schema that deliberately does not require modelling the request shape, and it grows with every kind added. Status is a subresource that can be dropped, so nothing a user depends on may live only there.

### The two forms, and the third

Both forms carry the same body, so the choice is an encoding, not a semantic. `spec` is the typed struct. `jsonSpec` is a required non-empty string holding the request body as written, which is what a string buys over a structured field: the document survives as the user pasted it. `gzipJsonSpec` is reserved for the same document gzipped and base64-encoded, for payloads that outgrow a plain string.

Exclusivity is enforced by `MinProperties=1` and `MaxProperties=1` on the struct rather than by a CEL rule, which is why the struct holds forms and nothing else. A rule spelling out `has(a) != has(b)` has to be rewritten into a three-way and then a four-way expression as forms are added, and each rewrite can be got wrong; a property count extends by itself. It is also the shape [provider-config.md](provider-config.md) already uses for `ValueSource` and for the authentication union.

A body sent as `jsonSpec` is sent byte for byte. A JSON dump is the document a user exported, not a re-encoding of it.

### What goes inside

The typed form mirrors the request body SigNoz accepts, field for field, so a dashboard's `objectTemplate.spec` is what the SigNoz editor's copy and download controls produce. Server-assigned identity is not part of it — that is `status.signozResourceMetadata`, per [core-status.md](core-status.md).

The body's schema version is the one field the two forms treat differently. A typed struct models exactly one version, so the typed form omits it and the operator sends the version its struct matches. A `jsonSpec` body is sent unaltered, so it must carry its own — the operator does not reach into it, and a body missing the version is rejected by SigNoz rather than by us.

The mirror is exact except where a body contains a union whose shape is selected by a sibling field. An auth domain's `config` is `{kind, spec}` where `spec` decodes as one of three provider types according to `kind` ([domain_config.go:61-64](https://github.com/SigNoz/signoz/blob/c40ebb027b2bac8b7fa5d219c7a16425adfe64da/pkg/types/authtypes/domain_config.go#L61-L64)). A CRD cannot type a field whose shape another field chooses, so the typed form carries the three variants' fields in one struct at that position, with CEL enforcing the per-kind subset, and the adapter assembles the body. The wire shape is unchanged. Kinds whose bodies hold no such union mirror it exactly.

Each kind declares its typed form as `<Kind>Config`, validated like any other field. Where a region of a body is genuinely open-ended, `+kubebuilder:pruning:PreserveUnknownFields` is scoped to that field rather than to the spec, so the shared controls stay strictly validated whatever the payload needs.

### One seam

Shared code asks for a body and gets one, without learning which form was set:

```go
// +kubebuilder:object:generate=false
type Resource interface {
    client.Object

    GetCoreSpec() *CoreSpec
    GetCoreStatus() *CoreStatus

    // RenderBody returns the bytes to send to SigNoz.
    RenderBody() (json.RawMessage, error)

    // Identity returns the key that finds this object in SigNoz, derived from
    // the rendered body so that it is the same on every form. See
    // idempotency.md.
    Identity() (string, error)
}
```

A body that will not parse fails at the seam, before anything else runs. Both `RenderBody` and `Identity` need to read it, and [idempotency.md](idempotency.md) resolves identity before the create, so a malformed `jsonSpec` stops the reconcile at its first step: `Terminal`, by the definition [core-status.md](core-status.md) gives it — desired state that no retry will fix. Nothing is written, no create is attempted, and no create-attempt annotation is recorded, so a document that cannot be read cannot leave an object of unknown outcome behind. Recovery is the spec edit that fixes the document, which is a new generation and therefore the ordinary way out of a stable state.

`observedHash` is computed over the rendered body parsed and re-marshalled, not over the bytes and not over the spec subtree. Canonicalising it is what makes the forms interchangeable in fact: reindenting a `jsonSpec` blob is not drift, and neither is moving identical content from `jsonSpec` to `spec`.

## Consequences

- Moving an object between forms is an in-place edit that produces no remote write. The edit advances `.metadata.generation`, the rendered body is unchanged, so the hash matches and the reconcile is a no-op.

- An edit to `interval` or `suspend` leaves the rendered body untouched, so a cadence or pause change does not present as drift either.

- A field SigNoz shipped this week is usable this week, through `jsonSpec`, with no operator release. The typed form catches up when we cut one, and the two forms coexist until then.

- Until an admission webhook exists, a malformed `jsonSpec` is caught at reconcile rather than at apply. The API server sees a valid string and CEL cannot parse JSON, so the resource applies cleanly and goes `Terminal` on the next reconcile. A webhook does not change that outcome; it moves the rejection forward to `kubectl apply`, where a body the schema does model is already rejected. The typed form needs no webhook.

- Printer columns must come from status. No path under `objectTemplate` resolves on a `jsonSpec` object, so anything `kubectl get` shows comes from `CoreStatus`, or from a field the operator lifts out of the body it rendered — desired state it already holds locally, on either form, which is not the remote mirror ruled out above.

- Schema defaults fire only on the typed form. Where a CRD default fills a field the user omitted, a typed resource sends it explicitly while a `jsonSpec` resource relies on SigNoz's own defaulting. The end state matches today; it will diverge the first time a server-side default changes.

- A credential cannot be read from a Secret on the encoded forms. The `value`/`valueFrom` shape needs a typed field to hang on, so an auth domain carrying an OAuth client secret has that secret in plaintext in the resource unless it uses the typed form. There is no substitution mechanism, and adding one is real scope.

- The word "spec" repeats down the path: twice before the body begins, and again for every `spec` the body has of its own — `spec.objectTemplate.spec.spec.panels` on a dashboard, `spec.objectTemplate.spec.config.spec.entityId` on an auth domain. Prose has to say which one it means, and `kubectl explain dashboard.spec.objectTemplate.spec` is a command people will fumble.

- `gzipJsonSpec` will need a decompressed-size ceiling enforced at admission when it lands. The etcd object limit bounds what can be stored, not what it expands to in the operator's memory.

- Adding a kind is declaring a `<Kind>Config` struct, its `objectTemplate` wrapper, an adapter supplying `RenderBody` and `Identity`, and the classification table [core-status.md](core-status.md) requires. Shared reconcile code does not change.

- A breaking SigNoz payload change of the kind already shipped once still means a new CRD version and a conversion for the typed form. Resources on `jsonSpec` are unaffected by our version, and affected by SigNoz's.

- We would revisit the shape if SigNoz stopped exposing a single request body per object, since one body per resource is what makes a single fence, a single seam and one hash work.


## Sources

- [Kubernetes — CronJob API (`jobTemplate`, nested `spec`)](https://kubernetes.io/docs/reference/kubernetes-api/batch/cron-job-v1/)
- [Kubernetes — PodTemplate API](https://kubernetes.io/docs/reference/kubernetes-api/core/pod-template-v1/)
- [Kubernetes — ResourceClaimTemplate API](https://kubernetes.io/docs/reference/kubernetes-api/resource/resource-claim-template-v1/)
- [Kubernetes — Ephemeral volumes (`volumeClaimTemplate`)](https://kubernetes.io/docs/concepts/storage/ephemeral-volumes/)
- [Kubernetes — DeviceClass API (`spec.config`, `opaque.parameters`)](https://kubernetes.io/docs/reference/kubernetes-api/resource/device-class-v1/)
- [Kubernetes — StorageClass API (`parameters`, `provisioner`, `reclaimPolicy`)](https://kubernetes.io/docs/reference/kubernetes-api/storage/storage-class-v1/)
- [OpenTelemetry Operator (`spec.config`)](https://opentelemetry.io/docs/platforms/kubernetes/operator/)
- [Keycloak Operator — realm import](https://www.keycloak.org/operator/realm-import)
- [Azure Service Operator — resources API (`operatorSpec`)](https://azure.github.io/azure-service-operator/reference/resources/v1api20200601/)
- [KEDA — Apache Kafka scaler (trigger metadata)](https://keda.sh/docs/latest/scalers/apache-kafka/)
- [KEDA — ScaledObject specification (`advanced`)](https://keda.sh/docs/latest/reference/scaledobject-spec/)
- [AWS Controllers for Kubernetes — deletion policy](https://aws-controllers-k8s.github.io/community/docs/user-docs/deletion-policy/)
