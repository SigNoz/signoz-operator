# ProviderConfig

> A namespaced object — or a cluster-scoped twin — naming one SigNoz backend and the header to authenticate with, referenced by name from every resource.

## Context

The operator writes to SigNoz over HTTP, and every write needs two things: *which* SigNoz to talk to, and *how to authenticate* to it. A cluster is rarely single-backend for long — staging runs against a self-hosted instance while production runs against a Cloud region, and different teams hold different credentials whose SigNoz-side permissions differ. Whatever carries the endpoint and credential is therefore on the write path of every reconcile, and the way a resource selects it decides how Kubernetes tenancy lines up — or fails to line up — with SigNoz tenancy.

SigNoz authenticates an API request with an HTTP header carrying a credential — `SIGNOZ-API-KEY: <key>` by default — and that key belongs to a service account, so it carries that account's permissions ([SigNoz — Service accounts](https://signoz.io/docs/manage/administrator-guide/iam/service-accounts/)). So the selection mechanism is not just plumbing: it determines whose credential a given namespace's resources write with, which is a security boundary, not a convenience. The header name is not fixed either: a reverse proxy or gateway in front of SigNoz may expect a different one (`x-api-key`, a tenant header) or an `Authorization: Bearer <token>` scheme. Which header authenticates a request, and whether its value carries a scheme prefix, is a property of how SigNoz was deployed, not something the operator can assume.

## Constraints

- **A SigNoz API key carries a fixed role, and nothing the operator does can narrow it.** The key belongs to a service account, which holds *"any managed or custom role available in your organization"* and receives *"the union of all transactions from its assigned roles"* ([SigNoz — Service accounts](https://signoz.io/docs/manage/administrator-guide/iam/service-accounts/)). A custom role can be scoped — a transaction is granted on *"All"* instances of a resource type or *"Only selected"* ones ([SigNoz — Roles](https://signoz.io/docs/manage/administrator-guide/iam/roles/)) — but that scoping is a property of the key, decided in SigNoz. Whatever holds the key writes with exactly its permissions, so the only lever the operator has is *which* key a resource writes with.
- **The authenticating header is deployment-specific.** The header *name* is configured by whoever deployed SigNoz or the gateway in front of it, and the value may need a scheme prefix (`Bearer`). The shape must let both be set, and must let a non-header scheme (OAuth2 client credentials, mTLS) be added later as an additive field, never a breaking change to the first.
- **Kubernetes RBAC on the credential Secret must decide who can use it.** Whoever can read the Secret writes with it; whoever cannot, cannot. That holds only if a resource names its credential object and the Secret resolves in one predictable namespace, rather than the operator searching the cluster for a match.
- **A resource must be able to choose its own backend.** Two resources in the same namespace may legitimately target different SigNoz instances; the namespace must not be the unit of backend selection.
- **Installs disagree on where the endpoint and credential should live.** A quickstart wants them inline; a GitOps or Vault-backed install wants nothing in the resource at all — ideally the whole connection in one externally-managed Secret. The object must serve both.
- **The reference must survive being added later.** Retrofitting a backend reference onto an already-shipped CRD forces an API version bump; the field has to exist from the first published version even before a second backend is in play.

Any solution therefore has to bind the credential to a Kubernetes-RBAC-controllable object whose Secret resolves in one predictable namespace, let an individual resource pick its target by naming it, admit more than one authentication scheme and more than one place to keep the secret, and be present in the API from day one.

## Considered options

### A single operator-wide credential from flags

The operator holds one endpoint and one credential, passed at startup, and every resource writes through it.

**Rejected.** One credential for the whole cluster means every namespace that can create a resource writes with the same SigNoz identity, and that identity has to be the union of what all of them need. A team that should only manage its own dashboards acts, through the operator, with the permissions of the most demanding tenant on the cluster. Kubernetes RBAC on the resource and SigNoz RBAC on the credential cannot be made to agree, because there is one credential for every tenant — and SigNoz's own per-instance role scoping becomes unusable, since a single key cannot hold a different scope per namespace. The convenience of no extra object is real but secondary; the credential-scoping failure is the disqualifier.

### A label selector over backend objects

Each resource carries a label selector; the operator resolves it to whichever backend objects match.

**Rejected.** A selector matches a credential by label rather than naming it, so which exact Secret — and whose RBAC governs it — turns on labels that can drift or collide, and the operator resolves a credential by searching rather than by name. A credential is named, not searched for: the direct `authenticationRef` KEDA uses for a `TriggerAuthentication` ([KEDA — authentication](https://keda.sh/docs/latest/concepts/authentication/)) gives one unambiguous owner and one Secret whose RBAC is the whole story.

### A cluster-wide config that pushes the credential into every reconcile

A single cluster-scoped configuration object holds the backend and credential, resolved centrally and handed to every reconciler.

**Rejected.** When the backend is resolved centrally and pushed down, the *namespace* decides the backend, so a resource cannot choose its own target — this fails the constraint directly, and it makes the staging-and-production-in-one-cluster case impossible without a second operator. The single point of configuration is tidy, but it buys that tidiness by taking backend choice away from the resource.

### Credentials as labeled Secrets, with no CRD

Keep the connection entirely in a Kubernetes `Secret` selected by a well-known label, as Argo CD does for cluster and repository credentials with `argocd.argoproj.io/secret-type: cluster` and `: repository` ([Argo CD — declarative setup](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)) — no custom resource at all.

**Accepted, in part.** Its core instinct is right and we keep it: credential material belongs in a `Secret`, so that native Secret RBAC and encryption-at-rest apply and the credential never sits in plaintext inside a spec. What a bare labeled Secret cannot give is a typed, validated endpoint, a place for TLS settings, a status subresource to report backend health on, or `kubectl` discoverability — all of which a backend definition wants. So we do not reject the Secret; we reject making it the *whole* object. The accepted design is a thin CRD that holds the non-secret configuration and *references* a Secret for the credential — the credential stays where Argo CD keeps it, and the typed surface lives in the CRD.

### A namespaced `ProviderConfig`, referenced by name

A namespaced custom resource holds the endpoint and an authentication block; every resource references it by name, resolved in the same namespace.

**Accepted.** A namespaced object plus its `Secret` makes Kubernetes RBAC and SigNoz RBAC line up: a team that can read the `Secret` in its namespace writes with that team's credential, and a team that cannot, cannot. The resource names the object directly and its Secret resolves in that same namespace — the model KEDA uses for `TriggerAuthentication`, which a `ScaledObject` names by reference and whose Secret is read in the workload's own namespace ([KEDA — authentication](https://keda.sh/docs/latest/concepts/authentication/)). Because the reference is 1:1 and names a concrete object, a resource picks its own backend, resolution is on-demand with no startup ordering to get wrong, and the target is self-documenting in the resource. Cloud and self-hosted coexist by pointing different objects at different endpoints, with no second operator deployment.

## Decision

Every resource names exactly one backend object via [`spec.providerConfigRef`](core-spec.md) — a namespaced `ProviderConfig` resolved in the resource's own namespace, or a cluster-scoped `ClusterProviderConfig`. The reference is **required**; a single-tenant install names one too. Either kind holds the endpoint and an authentication block whose method is, today, a single HTTP header; the credential is a value the block reads, inline or from a Secret.

### The kind is named `ProviderConfig`

The object plays the role Crossplane calls a [`ProviderConfig`](https://docs.crossplane.io/latest/managed-resources/managed-resources/): the endpoint-and-credentials configuration that managed resources reference to reach an external API, and we follow the `providerConfigRef` reference convention that goes with it. The name matters because a reader arrives with ecosystem vocabulary already loaded. A bare `Provider` is taken: in Crossplane and Cluster API a *Provider* is an installed controller or package that adds new APIs, and in External Secrets a `provider` is an inline field naming a backend type — *"exactly one provider must be configured"* ([SecretStore](https://external-secrets.io/latest/api/secretstore/)). A `Provider` kind here would read as an installed plugin, which this is not. `Connection` carries the opposite problem: it names a live, stateful thing, where this is a declarative pointer that holds no session. The peer projects filling this role land on config-flavoured names for the same reason — cert-manager's [`Issuer`](https://cert-manager.io/docs/concepts/issuer/) and External Secrets' `SecretStore`, *"namespaced and specifies how to access the external API"*.

### The reference carries `name` and an optional `kind`

```yaml
spec:
  providerConfigRef:
    name: prod                 # resolved in the resource's own namespace
    # kind: ProviderConfig     # default; or ClusterProviderConfig for the cluster-scoped twin
```

The reference deliberately carries no `namespace` field — the Secret resolves in the resource's own namespace, exactly as a KEDA `TriggerAuthentication` is named without a namespace and read in the `ScaledObject`'s. Sharing one credential across namespaces is explicit, by naming the cluster-scoped `kind`, not by a namespace-spanning reference. It is required on every resource: one with no `providerConfigRef` is rejected at apply. It carries a `kind`, defaulting to `ProviderConfig` and otherwise `ClusterProviderConfig`, the same `{name, kind}` pair Crossplane uses — *"managed resources must specify both name and kind"* ([Crossplane managed resources](https://docs.crossplane.io/latest/managed-resources/managed-resources/)). Crossplane fills an omitted reference with a `default` object; we require it instead, so that the backend a resource writes to is always visible in the resource:

```go
type ProviderConfigReference struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +optional
	// +kubebuilder:validation:Enum=ProviderConfig;ClusterProviderConfig
	// +kubebuilder:default=ProviderConfig
	Kind string `json:"kind,omitempty"`
}
```

### Authentication is a closed union of methods, and the first method is a header

One authentication block, with exactly one method set. The only method today is `header`, which sends one HTTP header; a non-header scheme (OAuth2, mTLS) is added later as a new optional field, never a change to an existing one.

```go
// Exactly one method must be set. Adding oauth2/mtls later is additive.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type Authentication struct {
	// Header sends one HTTP header carrying the credential.
	// +optional
	Header *HeaderAuth `json:"header,omitempty"`
}

// The credential is given exactly the way a Pod's env var is: value inline, or
// valueFrom a source — siblings on the header, not a nested wrapper.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="set exactly one of value or valueFrom"
type HeaderAuth struct {
	// Name of the header to send. Defaults to SIGNOZ-API-KEY.
	// +kubebuilder:default=SIGNOZ-API-KEY
	// +optional
	Name string `json:"name,omitempty"`

	// Scheme, if set, is prepended with a single space, producing "<scheme> <value>" —
	// e.g. Scheme "Bearer" with Name "Authorization" sends "Authorization: Bearer <value>".
	// +optional
	Scheme string `json:"scheme,omitempty"`

	// Value is the credential inline. When Scheme is set this is the bare token.
	// +optional
	Value string `json:"value,omitempty"`
	// ValueFrom sources the credential from a Secret (or ConfigMap) instead.
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}
```

The method is a sub-struct rather than a bare field so each scheme can grow its own options (the header's `name` and `scheme`; a future OAuth2 method's token URL, client id, and scopes) without disturbing the others. `MinProperties`/`MaxProperties` enforce "exactly one method". The `scheme` field is what keeps the `Bearer` case honest: the rotated secret stays a bare token, and `Authorization: Bearer ` is assembled at request time rather than baked into the stored value.

### Endpoint and credential both take `value` / `valueFrom`

`value` and `valueFrom` sit directly on the header, as they do on a Pod's `EnvVar` — never a nested `value.valueFrom` wrapper. The `valueFrom` source is the union `EnvVar` uses, trimmed to the two sources that apply here:

```go
// The analog of corev1.EnvVarSource, minus the pod-only fieldRef/resourceFieldRef.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type ValueSource struct {
	// +optional
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef,omitempty"`
	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
}
```

Reusing `corev1.SecretKeySelector` gives `{name, key}` that is name-only — same-namespace by construction, the property the constraints demand — and its deepcopy is already generated. `fieldRef` and `resourceFieldRef` are pod- and container-only and are left out. Kubernetes already forbids setting both on an `EnvVar`: `valueFrom` *"Cannot be used if value is not empty"* ([Pod API reference](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/)). The `HeaderAuth` CEL rule matches that and goes one step further by requiring that one of them be set, so a header declared with no credential at all is rejected at apply instead of resolving to an empty string.

The endpoint takes the same `value` / `valueFrom`, reusing `ValueSource`, so an install that keeps backend config out of the CR can source the URL from the same Secret (or Vault entry) as the token — one object carrying the whole connection:

```go
// Endpoint mirrors the credential: inline value, or valueFrom a Secret/ConfigMap.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="set exactly one of value or valueFrom"
type Endpoint struct {
	// +kubebuilder:validation:Pattern=`^https?://.+$`
	// +optional
	Value     string       `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}
```

The `Pattern` validates an inline URL; a sourced one is checked when read. An install that externalizes everything then keeps both endpoint and token in one Secret:

```yaml
spec:
  endpoint:
    valueFrom: {secretKeyRef: {name: signoz-conn, key: endpoint}}
  auth:
    header:
      valueFrom: {secretKeyRef: {name: signoz-conn, key: token}}
```

One optional block remains, `tls`, for a self-hosted endpoint behind a private CA — a Cloud endpoint needs none of it:

```go
type TLSConfig struct {
	// InsecureSkipVerify disables server-certificate verification.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
	// CASecretRef names a Secret key holding a CA bundle to trust.
	// +optional
	CASecretRef *corev1.SecretKeySelector `json:"caSecretRef,omitempty"`
}
```

Kubernetes has no CRD-ready TLS type to adopt: `corev1` offers only the `kubernetes.io/tls` Secret convention (keys `tls.crt` / `tls.key` / `ca.crt`), and `rest.TLSClientConfig` is a runtime type over file paths and raw bytes, not Secret references. So the struct is ours — but its one reference reuses `corev1.SecretKeySelector`, so the CA Secret resolves same-namespace by construction, exactly like the credential. A standard TLS Secret keys its CA under `ca.crt`, so `caSecretRef: {name: signoz-ca, key: ca.crt}` is the usual form.

A complete object, with the endpoint inline and the credential from a Secret:

```yaml
apiVersion: resources.signoz.io/v1alpha1
kind: ProviderConfig
metadata: {name: prod, namespace: platform}
spec:
  endpoint:
    value: https://eu.signoz.cloud
  auth:
    header:                                 # name defaults to SIGNOZ-API-KEY
      valueFrom:
        secretKeyRef: {name: signoz-api, key: token}
  # optional: tls.insecureSkipVerify, tls.caSecretRef
```

The header method covers every scheme a SigNoz deployment might sit behind:

```yaml
# Default API-key header:  SIGNOZ-API-KEY: <token>
auth:
  header:
    valueFrom: {secretKeyRef: {name: signoz-api, key: token}}

# Custom header name:  x-api-key: <token>
auth:
  header:
    name: x-api-key
    valueFrom: {secretKeyRef: {name: signoz-api, key: token}}

# Bearer scheme:  Authorization: Bearer <token>   (Secret holds only the token)
auth:
  header:
    name: Authorization
    scheme: Bearer
    valueFrom: {secretKeyRef: {name: signoz-api, key: token}}

# All inline — quickstart / single-tenant
auth:
  header:
    value: "sk-..."
```

### A cluster-scoped `ClusterProviderConfig` is a first-class twin

`ProviderConfig` (namespaced) and `ClusterProviderConfig` (cluster-scoped) are two kinds over **one shared spec** — the same endpoint, auth block, and TLS. They differ only in scope and in where their Secret and ConfigMap references resolve. This is the pattern every peer ships: KEDA's `TriggerAuthentication`/`ClusterTriggerAuthentication` share a spec, as do Crossplane's `ProviderConfig`/`ClusterProviderConfig`, cert-manager's `Issuer`/`ClusterIssuer`, and External Secrets' `SecretStore`/`ClusterSecretStore`.

The scoping rule for secrets is what makes the cluster twin safe:

| Kind | Scope | Where its Secret / ConfigMap references resolve |
|---|---|---|
| `ProviderConfig` | namespaced | the `ProviderConfig`'s own namespace |
| `ClusterProviderConfig` | cluster | the **operator's** namespace |

A cluster-scoped object has no namespace of its own, so its name-only Secret reference resolves in the operator's namespace — as with a KEDA `ClusterTriggerAuthentication`, whose Secrets *"must be in the same namespace as KEDA is deployed in"* ([KEDA — authentication](https://keda.sh/docs/latest/concepts/authentication/)). The shared credential is therefore one a cluster administrator places beside the operator, and a namespaced resource that names a `ClusterProviderConfig` uses that credential without holding a copy of it.

This splits the two audiences cleanly. A team self-serves a namespaced `ProviderConfig` with a Secret in its own namespace, governed by that namespace's RBAC. A platform owner publishes a `ClusterProviderConfig` for a backend the whole cluster should reach — the "one org SigNoz" case — and creating it requires cluster-scoped RBAC, which is the correct gate for a cluster-wide credential.

## Consequences

- **Kubernetes RBAC becomes the control over SigNoz credentials.** Granting a team `get` on a `Secret` in their namespace is what lets their resources write to that backend; there is no separate credential-authorization surface to keep in sync.

- **An inline credential is a real exposure, and the operator has to compensate.** `auth.header.value` puts the credential in the spec — visible in `kubectl get -o yaml`, stored in etcd (plaintext unless encryption-at-rest is enabled), and captured in any GitOps-rendered manifest. It is supported for the quickstart case, but the docs steer users to `valueFrom`, and the operator **must** keep the resolved credential out of every `status`, condition message, event, and log line. A credential that reaches a condition message is copied wherever conditions are copied, which is everywhere.

- **A backend shared across namespaces has a home: a `ClusterProviderConfig`.** Rather than re-declaring a `ProviderConfig` and `Secret` in every namespace, a platform owner publishes one cluster-scoped object whose credential lives beside the operator. The cost moves rather than vanishing — that credential is now the operator's cluster-wide identity, so it must be scoped in SigNoz to what every namespace referencing it is allowed to do.

- **A new authentication scheme is additive.** The `header` method covers key headers and `Bearer` today; adding OAuth2 or mTLS is a new optional field under `Authentication` plus one arm in the resolver, and existing objects keep validating unchanged.

- **The reference is required from the first published version.** Because every resource names its backend from `v1alpha1`, supporting a second backend later is configuration, not an API change; its `kind` even moves a resource between the namespaced and cluster-scoped twin without a schema change.

- **Even a single-backend install declares one object.** The smallest deployment still creates one `ProviderConfig` (or `ClusterProviderConfig`) and names it — a little more YAML for the trivial case, in exchange for a backend that is always explicit in the resource that uses it.

- **We would revisit the reference model** if a legitimate need for one resource to fan out to several backends appeared. A selector would first have to answer which exact credential a label match resolves to, and whose RBAC governs it.

## Open questions

- **What `ProviderConfig.status` reports, and whether the operator probes the backend.** A typed object earns a status subresource, and a `Ready` condition on the `ProviderConfig` would let a user tell "my credential is wrong" apart from "my dashboard is wrong" without reading every resource that references it. Nothing here specifies what would set it: an active health probe on an interval, or a passive record of the last resolution and the last request outcome. Until that is decided, a broken backend is diagnosed from the `Terminal` and `Recoverable` conditions on the resources themselves, per [core-status.md](core-status.md).

- **How a credential rotation reaches in-flight resources.** The credential is read from a Secret at request time, so a rotated Secret takes effect on the next reconcile — but a resource sitting on `Terminal` after authentication failures is not requeued by a Secret change, only by a spec edit or the next `interval`. Whether the operator should watch referenced Secrets and requeue their dependents is not settled.

## Sources

- [Crossplane — Managed resources (`providerConfigRef`, ProviderConfig types)](https://docs.crossplane.io/latest/managed-resources/managed-resources/)
- [cert-manager — Issuer / ClusterIssuer](https://cert-manager.io/docs/concepts/issuer/)
- [External Secrets Operator — SecretStore](https://external-secrets.io/latest/api/secretstore/)
- [KEDA — Authentication (TriggerAuthentication / ClusterTriggerAuthentication)](https://keda.sh/docs/latest/concepts/authentication/)
- [Argo CD — Declarative setup (credential Secrets by label)](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)
- [SigNoz — Service accounts and API keys](https://signoz.io/docs/manage/administrator-guide/iam/service-accounts/)
- [SigNoz — Roles and custom roles](https://signoz.io/docs/manage/administrator-guide/iam/roles/)
- [Kubernetes — Pod API reference (`EnvVar`)](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/)
