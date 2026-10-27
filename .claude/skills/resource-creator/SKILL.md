---
name: resource-creator
description: >-
  End-to-end workflow for adding a new mirrored kind (a CRD that mirrors one SigNoz API object) to signoz-operator through the skaff OpenAPI codegen pipeline. Use this whenever the task is to "add", "mirror", "create", "generate", "scaffold" or "wire up" a new kind in `resources.signoz.io/v1alpha1` from a SigNoz API endpoint, even if the user only names the API ("notification channels", "LLM pricing rules", "dashboard views") without saying "skaff", "CRD" or "codegen". It covers reading the endpoint in the OpenAPI spec, declaring the kind in skaff.yml, running the pipeline through skaff-runner, hand-completing the adapter's Find, writing the samples, the README row, the `make checks` gate, an e2e suite, and the commit/PR split. Reach for it before hand-writing any types, objects, controllers or main.go wiring: all of that is generated.
---

# Adding a mirrored kind to signoz-operator

Every mirrored kind is **generated**, not hand-written. [`SigNoz/skaff`](https://github.com/SigNoz/skaff) reads the SigNoz OpenAPI spec plus a per-kind block in `skaff.yml` and emits the kind's `zz_generated_*` files, scaffolds it through kubebuilder, and writes its `cmd/main.go` registration. Adding a kind means *declaring* it, *running the pipeline*, and finishing the two things the pipeline leaves to a human: the adapter's `Find` and the sample.

Nine kinds already shipped this way (`User` is the pilot; `Role`, `RoutePolicy`, `ServiceAccount`, `PlannedMaintenance` and `Rule` came in one batch in #30). Follow their shape; don't invent a new one.

## Mental model: the layers

Per kind, in the order the pipeline writes them:

| Layer | File | Who writes it |
|---|---|---|
| API types (`<Kind>ObjectSpec`, `<Kind>ObjectTemplate`, `<Kind>Spec`/`Status` with `CoreSpec`/`CoreStatus` inline) | `api/resources/v1alpha1/zz_generated_<kind>_types.go` | skaff `types` |
| Object wrapper (`Body`, `Identity`, `Hash`, `UpdatableFields`, method+path) | `internal/resources/objects/zz_generated_<kind>_object.go` | skaff `objects` |
| Adapter (`commonTransport` + `Find`) | `internal/resources/adapters/<kind>_adapter.go` | skaff `adapters` writes a stub **once**; you complete `Find` |
| Controller (Get → wrap → injected `CommonReconciler`) | `internal/controller/resources/zz_generated_<kind>_controller.go` | skaff `controllers` |
| Registration block, `PROJECT` entry, CRD/sample kustomization lines, sample stub | `cmd/main.go`, `PROJECT`, `config/crd/kustomization.yaml`, `config/samples/` | skaff `controllers`, through `kubebuilder create api` |
| Deepcopy, CRD, cluster role | `zz_generated.deepcopy.go`, `config/crd/bases/resources.signoz.io_<plural>.yaml`, `config/rbac/clusterrole.generated.yaml` | controller-gen, via primus |

The shared engine (`internal/resources/reconcilers`, the `Object`/`Adapter` seam in `internal/resources/`, `internal/jsonbody`) is the same for every kind and is not touched when adding one. Why the spec is shaped as `objectTemplate.spec` / `objectTemplate.jsonSpec` is in [`docs/resources.md`](../../../docs/resources.md); why `Find` exists at all is in [`docs/idempotency.md`](../../../docs/idempotency.md).

Never hand-edit a `zz_generated_*` file, a CRD base, the cluster role, `PROJECT`, or a kind's `cmd/main.go` block; change `skaff.yml` (or skaff's templates, upstream) and regenerate. Keep every `+kubebuilder:scaffold:*` marker.

## Inputs (where things live)

- **Spec:** `tmp/spec/openapi.yml` (gitignored), the one file the pipeline reads. It is pinned: the committed kinds were generated from a *constructed* spec (signoz main with SigNoz/signoz#12112 merged in), so before you start, note the size and sha the pipeline prints and confirm it reproduces the committed tree (Step 2 shows how). If the new endpoint is missing from the pinned spec, the pin has to move first, and moving it re-renders the existing kinds too: do that as its own catch-up change with the **spec-syncer** skill, then add the kind on top. How to prepare a spec is in the [skaff-runner skill](../skaff-runner/SKILL.md#prepare-the-spec-first).
- **Config:** `skaff.yml` at the repo root (you edit it in Step 1).
- **Product code:** [`SigNoz/signoz`](https://github.com/SigNoz/signoz) on GitHub (read only; browse it with `gh` or a throwaway clone), for what the spec cannot tell you: `pkg/types/**` for shapes and valid values, `pkg/sqlmigration/**` for uniqueness constraints.
- **Tools:** skaff and controller-gen come from primus (`PRIMUS_HOME`; run the **primus-setter** skill if it is unset). Scaffolding a new kind also needs `kubebuilder` on `PATH`.

## Step 0 — Read the endpoint in the spec

Find the collection and item paths (`/api/vN/<collection>` and `/api/vN/<collection>/{id}`) and note, from their operations:

- **create** (`POST` on the collection): its request schema becomes `<Kind>ObjectSpec`; its response tells you where the new id lives (almost always `data.id`, inside the `{status, data}` envelope).
- **read / update / delete** on `{id}`. The update request schema decides `UpdatableFields`, the fields the operator compares and pushes. A kind with no update operation, or a create that is a `PUT` (like `llm_pricing_rules`), does not fit the shape the existing kinds use; stop and raise it rather than bending `skaff.yml`.
- **list** (`GET` on the collection): whether `data` is a flat array, whether it takes a filter parameter, whether it paginates. This decides `Find` in Step 3.
- **identity**: the create-body field that names the object (`name` for most kinds, `email` for `User`, `alert` for `Rule`, because `PostableRule` has no `name`). It must come from desired state, never from a server response. Check `pkg/sqlmigration` for whether SigNoz enforces it as unique; if it does not, `Find` alone carries idempotency and a same-named object made in the UI makes the kind go `Terminal` (ambiguous).

The installed SigNoz image in the e2e suite can lag the spec. If a route matters, probe it on a running instance (`GET /api/v1/version` says what is running); an unrouted path answers `200` with the web app, not `404`.

## Step 1 — Declare in `skaff.yml`

Add one block under `resources:`, keyed by the snake_case name (`route_policy`, `planned_maintenance`), which skaff turns into the Kind (`RoutePolicy`). Copy the nearest existing block:

```yaml
resources:
  <name>:
    identity:
      field: name
    create:
      path: /api/v1/<collection>
      method: POST
      status:
        signozResource:
          id: data.id
    read:
      path: /api/v1/<collection>/{id}
      method: GET
    update:
      path: /api/v1/<collection>/{id}
      method: PUT
    delete:
      path: /api/v1/<collection>/{id}
      method: DELETE
    schema:
      ignores:
        - data
        - status
        - createdAt
        - createdBy
        - updatedAt
        - updatedBy
```

`skaff.yml` only carries what the spec cannot say: identity, the operation mapping, where the created id lives, and fields to leave out of the typed form. Never declare field schemas here; they come from the spec.

**`schema.ignores`:**
- The envelope and server audit fields (`data`, `status`, `createdAt`, …) are the default set.
- To keep a field out of the typed `objectTemplate.spec`, add its **OpenAPI property name**. It stays settable through `jsonSpec`. Existing uses: `userRoles` and `frontendBaseUrl` on `user`, `serviceAccountRoles` on `service_account`, `authNProviderInfo` on `auth_domain`, `generateName` on `saved_view`/`dashboard`, `state` on `rule`. Reach for it for relationship fields managed elsewhere, response-only fields, and fields the CRD cannot model usefully.

## Step 2 — Run the pipeline

Run the **skaff-runner** skill's script. It takes no arguments and reads `tmp/spec/openapi.yml`:

```sh
bash .claude/skills/skaff-runner/scripts/run-pipeline.sh
```

`DRY_RUN=1` previews the invocations. See the [skaff-runner skill](../skaff-runner/SKILL.md) for the guarantees it makes; it always runs the latest skaff release.

It regenerates **every** declared kind. For a new kind, expect `git status` to show exactly: new `zz_generated_<kind>_types.go`, `zz_generated_<kind>_object.go`, `zz_generated_<kind>_controller.go`, `<kind>_adapter.go`, `resources.signoz.io_<plural>.yaml` and `resources_v1alpha1_<kind>.yaml`, plus edits to `PROJECT`, `cmd/main.go`, `zz_generated.deepcopy.go`, `config/crd/kustomization.yaml`, `config/samples/kustomization.yaml`, `config/rbac/clusterrole.generated.yaml` and `skaff.yml`. Any **other** kind showing a diff means either the pinned spec is not the one that produced the committed tree, or a newer skaff release re-renders it. Run the **spec-syncer** preview against the current pin to tell which; either way, land that re-render as its own catch-up change with spec-syncer before adding the kind on top.

Then look at what the typed form became:
- `stays a passthrough` lines on stderr, and `grep -n apiextensionsv1.JSON api/resources/v1alpha1/zz_generated_<kind>_types.go` for the silent ones. A passthrough field is validated by SigNoz, not at apply. Fine for genuinely open regions; for a field users will set often, consider whether an `ignores` entry (jsonSpec-only) is the more honest surface.
- `UpdatableFields()` in the object file. It should match the update request schema. An empty or surprising list means the update operation in `skaff.yml` points at the wrong schema.

## Step 3 — Complete `Find`

The pipeline ends by listing `!! stub Find` for `internal/resources/adapters/<kind>_adapter.go`. This file is handwritten from here on (skaff never overwrites it), and the stub `return nil, nil` compiles, registers and reconciles while answering "never found" forever, so no gate catches it.

Add the collection path constant and pick the shape that matches the list endpoint from Step 0:

| List endpoint | Model | `Find` |
|---|---|---|
| flat `data` array, no filter, exact match | `role_adapter.go` (also `authdomain`, `routepolicy`, `plannedmaintenance`, `serviceaccount`, `rule`) | `return findByField(ctx, c, obj, <kind>sPath, "<identity field>")` |
| case-insensitive identity | `user_adapter.go` | own loop with `strings.EqualFold` |
| server-side filter parameter | `savedview_adapter.go` | query the filter, still compare exactly |
| paginated | `dashboard_adapter.go` | walk pages with an empty-page guard |

```go
const channelsPath = "/api/v1/channels"

func (*ChannelAdapter) Find(ctx context.Context, c clients.SigNoz, obj resources.Object) ([]*v1alpha1.SigNozResource, error) {
	return findByField(ctx, c, obj, channelsPath, "name")
}
```

Return every match; the engine decides what zero, one or several mean. If you write a new helper shared across adapters, give it table-driven tests next to it, like `common_find_test.go` (see `.claude/rules/go-tests.md`).

## Step 4 — Samples

The pipeline also lists `!! sample is still kubebuilder's stub` for `config/samples/resources_v1alpha1_<kind>.yaml`. Replace the stub with two documents, modelled on `resources_v1alpha1_user.yaml` and `resources_v1alpha1_role.yaml`:

1. `<kind>-sample`: the typed form, `objectTemplate.spec`, with a comment that the API server validates it at apply.
2. `<kind>-sample-json`: the `jsonSpec` form, the request body sent verbatim, showing any field `ignores` kept out of the typed form.

Both carry the `app.kubernetes.io/managed-by: kustomize` and `app.kubernetes.io/name: signoz-operator` labels and `providerConfigRef: {name: providerconfig-sample}`. Take values from the product's own types and test fixtures in `SigNoz/signoz`, not from guesses: an enum or verb the server rejects makes a sample that applies cleanly and then goes `Terminal`.

Check the typed sample against the generated CRD with a server-side dry run, for example on the e2e cluster:

```sh
make setup-e2e-env
kubectl --kubeconfig tests/tmp/kubeconfig apply --dry-run=server -n signoz-operator-e2e -f config/samples/resources_v1alpha1_<kind>.yaml
make cleanup-test-e2e
```

That environment deploys this checkout's CRDs, so rebuild it (`make setup-e2e-env` again) after regenerating.

## Step 5 — README

Add the kind to the **Custom Resources** table in `README.md` (`| \`<Kind>\` | A <one-line description>. |`), grouped with its neighbours. If the kind needs a design note (a union carried as passthrough, a known drift gap, no server-side uniqueness worth calling out), write it under `docs/` with the **docs-writer** skill.

## Step 6 — Gates

```sh
make checks
```

That is `go-checks` (fmt, deps, lint, test), `controllergen-checks`, `kyaml-checks` and `py-checks`, all through primus, the same targets CI runs (see `.claude/rules/go-checks.md`). Two things to expect:
- `kyaml-checks` reformats files in place and fails the run when it changed something. The kubebuilder sample stub and hand-written samples are the usual cause; review the reformat and run it again.
- After `controllergen-checks` the tree must be clean; CI's `objects` and `manifests` jobs fail on any generated-file drift.

`vendor/` is gitignored, so a `go mod vendor` from the scaffold is not part of the change. A `go.mod`/`go.sum` diff is, and `go-deps` checks it.

## Step 7 — e2e suite

The suite in `tests/` runs the operator against a real SigNoz in Kind; read [`tests/README.md`](../../../tests/README.md) and follow `.claude/rules/pytest.md`. `tests/e2e/tests/dashboard/01_dashboard.py` is the model:

- `tests/fixtures/<kind>s.py`: the `<KIND>S_BASE_URL`, a body builder that takes the testdata path as an argument, a `<kind>_manifest(...)` plain function, and a `<kind>_name` fixture that deletes any SigNoz object of that name at teardown (an orphaned or adopted one outlives its resource). Register it in `tests/conftest.py` `pytest_plugins`.
- `tests/testdata/<kind>.json` if the body is non-trivial.
- `tests/e2e/tests/<kind>/01_<kind>.py`, covering what differs per kind: create records the id, a spec edit reaches SigNoz, out-of-band drift on an updatable field is reverted, suspend holds, `reclaimPolicy` `Delete` and `Orphan`, adoption of an existing object, and an unparseable `jsonSpec` is `Terminal`. Reuse `await_condition`, `eventually` and the constants in `fixtures/resources.py`.

Run it narrowed to the new suite:

```sh
make test-e2e-reuse E2E_TARGET=e2e/tests/<kind>/01_<kind>.py
make cleanup-test-e2e
```

Assert exact status codes with `HTTPStatus`; a wrong route answers `200` with HTML.

## Step 8 — Commit and PR

On a `feat/<kind>` branch, split the work so each commit reviews on its own (PRs are squash-merged, so the split is for the reviewer):

1. **codegen:** `skaff.yml` and everything the pipeline wrote (`zz_generated_*`, CRD base, cluster role, `PROJECT`, `cmd/main.go`, the two kustomizations, `<kind>_adapter.go` as stubbed).
2. **wiring:** the completed `Find`, the samples, the `README.md` row, any `docs/` note.
3. **e2e:** `tests/`.

Commit messages follow the repo's conventional style, for example `feat(resources): mirror <Kind> through a skaff-generated kind`. Open the PR following `.claude/rules/pull-requests.md`, which owns the template and body format. The e2e workflow only runs on PRs labelled `safe-to-e2e`.
