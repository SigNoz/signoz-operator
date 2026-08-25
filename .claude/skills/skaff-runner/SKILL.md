---
name: skaff-runner
description: >-
  Run the ENTIRE skaff operator codegen pipeline deterministically, via a bundled script — every kind declared in skaff.yml, all four generators in the required order (types → objects → adapters scaffold → controllers), then controller-gen through primus so deepcopy and the CRDs match. Use whenever you need to (re)generate the operator's per-kind files — after editing skaff.yml, bumping the OpenAPI spec, or adding a mirrored kind. Also covers preparing the pinned spec the pipeline reads from tmp/spec/openapi.yml: from a signoz branch, from a spec constructed by regenerating signoz's code, or from signoz's origin/main by default. It regenerates every declared kind, never a single one; it does not edit skaff.yml, hand-complete adapter Find logic, flesh out samples, or run the build/lint/test gate.
---

# skaff operator pipeline runner

`scripts/run-pipeline.sh` runs the whole skaff operator pipeline in one deterministic pass, so the order and flags are never retyped by hand. It regenerates **every** kind declared in `skaff.yml` — skaff has no single-kind mode, so "just regenerate Rule" is not a thing you can ask for.

## Prepare the spec first

The pipeline reads exactly one file — `tmp/spec/openapi.yml` — and nothing else decides what the generated kinds look like. Put the spec there before running; the script refuses to guess. `tmp/` is gitignored, so this is a pinned local input, not something you commit.

**If you were given instructions on which spec to use, follow them.** "Generate from branch X", "use the spec with PR N merged in", "regenerate from the code on my branch" all override the default.

**If you were given none, take signoz's current main:**

```sh
mkdir -p tmp/spec
git -C ../signoz fetch --quiet origin
git -C ../signoz show origin/main:docs/api/openapi.yml > tmp/spec/openapi.yml
```

Read `origin/main` rather than the local `main` branch — a signoz checkout kept for reference tends to sit well behind it, and generating against a stale spec silently rewrites kinds to a contract the product has already moved past.

**From a branch** that already carries the spec, it's the same read with the ref you were handed:

```sh
git -C ../signoz show <ref>:docs/api/openapi.yml > tmp/spec/openapi.yml
```

**Constructing one** is for when the spec you need exists on no single ref — it wants an open PR's schema changes combined with main, or a branch's committed spec lags its own handlers. signoz generates the spec from the code:

```sh
(cd ../signoz && go run ./cmd/community generate openapi)   # writes ../signoz/docs/api/openapi.yml
cp ../signoz/docs/api/openapi.yml tmp/spec/openapi.yml
```

It needs no config or database and is deterministic — re-running it on an unchanged tree rewrites the file byte-identically. Arrange the signoz tree however you were asked (merge the PR branch, check out the ref) *before* generating, and restore the checkout afterwards, since this dirties it.

This is exactly how the committed `Rule` kind was produced: main with SigNoz/signoz#12112 merged in, then regenerated — which is why the spec behind it matches no ref you can name.

## Run it

```sh
bash .claude/skills/skaff-runner/scripts/run-pipeline.sh
```

No arguments — it reads `tmp/spec/openapi.yml` and prints that file's size and hash, so the generated tree is always traceable to a spec you can identify.

- **`SKAFF_VERSION=vX.Y.Z`** — which skaff release primus fetches (default: `latest`).
- **`SKAFF_REFRESH=1`** — delete `tmp/bin/skaff` first, forcing a re-download.
- **`DRY_RUN=1`** — print each invocation, provisioning included, instead of running them (preview).
- **`SKIP_CONTROLLERGEN=1`** — skip the trailing primus `controllergen` runs.

What it guarantees (so callers don't have to remember):
- generators run in order — `types`, `objects`, `adapters` (scaffold), then `controllers` — each into its conventional directory, with `--module` and repo root derived automatically (`go.mod`, `git`). The `adapters`-before-`controllers` half of that order is load-bearing: the `cmd/main.go` block `controllers` writes calls `adapters.New<Kind>Adapter()`, so generated the other way round the tree won't compile until the adapter appears;
- `zz_generated_*` files are overwritten; `<kind>_adapter.go` is **write-if-missing** — a handwritten adapter is never touched;
- `controllers` drives kubebuilder: a kind missing from PROJECT is scaffolded through `kubebuilder create api` (needs `kubebuilder` on PATH; its stub types/controller files are dropped in favor of the generated ones, RBAC roles/sample/kustomization wiring kept, a pre-existing sample never overwritten), and every kind's `cmd/main.go` registration block is rewritten into the house `NewCommonReconciler` shape — so don't hand-edit those blocks, edit the template;
- skaff comes from primus and nowhere else — primus downloads the release into `tmp/bin` and the generators run that binary, so the version rendering this repo is the one CI and every other machine gets, never somebody's local build. The script prints the version it resolved. Note primus keeps a binary already sitting at that path rather than re-downloading, so a newer release needs `SKAFF_REFRESH=1`; that is also what clears out anything that got there by other means;
- controller-gen runs last, as the same two primus `controllergen` invocations CI uses — `object paths="./..."` then the `rbac`/`crd`/`webhook` set — so `zz_generated.deepcopy.go` and `config/crd/bases/*.yaml` always match the regenerated types. Going through primus rather than a bare `controller-gen` is what pins the version and flags CI runs, which matters here because CI fails the `objects` and `manifests` jobs on any generated-file drift. Needs `PRIMUS_HOME`; the script checks for it up front, and the **primus-setter** skill sets it up if it's missing.

## After it runs

The script ends by listing the two things it deliberately leaves to a human, so read its last lines rather than going looking:

1. a stub `Find` (`return nil, nil`) in `internal/resources/adapters/<kind>_adapter.go`. This one is worth care because it fails *silently*: the kind compiles, registers and reconciles, and simply answers "never found" forever, so no gate catches it.
2. a `config/samples/resources_v1alpha1_<kind>.yaml` still carrying kubebuilder's `TODO(user)` stub. The User sample is the model for both template forms.

Also expect, and don't be alarmed by:
- `stays a passthrough` notes on stderr — a field skaff could have typed but didn't, because a `oneOf`'s variants disagree or every candidate type name is taken. It rides as `apiextensionsv1.JSON` instead. Note these are the *loud* passthroughs only: a field whose schema a structural CRD simply cannot express (a cyclic `$ref`, say) is demoted silently, so `grep -rn apiextensionsv1.JSON api/resources/v1alpha1/` is the way to see them all;
- a `go.mod`/`vendor/` diff when a **new** kind was scaffolded — `kubebuilder create api` resolves dependencies and skaff re-runs `go mod tidy && go mod vendor` behind it;
- re-renders of every other declared kind in `git status`, since the pipeline touches them all. These are usually byte-identical — but only because the pinned spec still matches the one that produced the committed state. A kind whose contract has moved since it was last generated shows a **real** diff here, so read the diff before dismissing it as noise. When an unexpected kind changes, suspect what you put in `tmp/spec/openapi.yml` rather than skaff.

Then run the gates in @.claude/rules/go-checks.md
