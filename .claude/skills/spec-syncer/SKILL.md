---
name: spec-syncer
description: >-
  Re-run the full skaff operator pipeline against a newer SigNoz OpenAPI spec to catch the mirrored kinds up on spec drift, then open a catch-up PR with the regenerated types, objects, deepcopy and CRDs. Use when the task is "the upstream openapi spec changed", "regenerate the operator from the latest spec", "sync the generated kinds to SigNoz/signoz main", "pick up the new spec", "an upstream PR merged, regenerate Rule", or a periodic spec-drift catch-up. It regenerates every kind already declared in skaff.yml — it does not add a kind (use resource-creator for that), and makes no skaff.yml edits.
---

# Sync the generated kinds to the upstream OpenAPI spec

The upstream spec (`SigNoz/signoz` → `docs/api/openapi.yml`) changes often, and every mirrored kind's typed spec, updatable fields, deepcopy and CRD are rendered from it. This skill moves the pinned spec forward, regenerates everything, and lands the drift as one **catch-up PR**. It is the **resource-creator** flow minus declaring and registering a kind: same pipeline, no `skaff.yml` change, all declared kinds at once.

The pipeline itself belongs to the **skaff-runner** skill — this skill decides *which* spec to feed it, previews what that spec would do, and judges the result. Never retype the generator invocations.

## Prerequisites

- `PRIMUS_HOME` set — skaff and controller-gen both come from primus. If it isn't, use the [primus-setter](../primus-setter/SKILL.md) skill.
- The signoz checkout at `../signoz` (read-only here: fetch and `git show` only, unless you are constructing a spec — see Step 1).
- `gh` authenticated with SigNoz org access.

## Steps

### 1. Pick the spec, and record where it came from

The committed kinds were not necessarily generated from a plain signoz ref. `Rule` rides on **SigNoz/signoz#12112**, which was still open when it was generated, so the committed tree came from signoz main *with that PR merged in*, constructed through `go run ./cmd/community generate openapi`. Feeding plain main to the pipeline reverts `Rule` to the v1 contract, and nothing warns.

So before choosing, settle the provenance. The PRs that last regenerated the kinds say in their bodies which spec they used (#30 is where the #12112 dependency is recorded):

```sh
gh pr view 12112 --repo SigNoz/signoz --json state,mergedAt   # still OPEN → main alone is not enough
git log --format='%h %s' -5 -- 'api/resources/v1alpha1/zz_generated_*_types.go'  # PRs that last regenerated kinds
```

- **Told which spec to use** (a branch, "with PR N merged", "from my branch") — follow that.
- **Otherwise** — signoz `origin/main`, plus every still-open upstream PR the committed kinds depend on, merged in and regenerated. If none are open any more, plain `origin/main` is the target.

Prepare the candidate **next to** the pinned spec, not over it, so the current pin stays available as a baseline and for restoring. The skaff-runner skill's **Prepare the spec first** section has the exact commands for each source (main, a branch, a constructed spec); write the result to `tmp/spec/openapi.next.yml`. For plain main:

```sh
git -C ../signoz fetch --quiet origin
git -C ../signoz show origin/main:docs/api/openapi.yml > tmp/spec/openapi.next.yml
SPEC_SHA=$(git -C ../signoz rev-parse --short origin/main)
echo "syncing against SigNoz/signoz@$SPEC_SHA"
```

Always read `origin/main`, never the local `main` — the reference checkout lags it badly. Note `SPEC_SHA` and any PRs you merged in; they go in the PR body, which is the only place this provenance is recorded.

### 2. Confirm the baseline, then preview the drift

`scripts/preview-drift.sh` renders the two spec-reading generators (`types`, `objects`) with the latest skaff release into `tmp/spec-preview/` (gitignored, replaced on every run) and compares each kind with what is committed. It never touches the checkout, and it writes a unified diff for each changed kind to `tmp/spec-preview/<kind>.diff`.

```sh
bash .claude/skills/spec-syncer/scripts/preview-drift.sh                            # the current pin
bash .claude/skills/spec-syncer/scripts/preview-drift.sh tmp/spec/openapi.next.yml  # the candidate
```

**The baseline run tells you what skaff alone changes.** Both scripts always use the latest skaff release, so a kind that does not report `same` against the current pin is being re-rendered by a newer skaff, not by the spec. That is expected and goes into the sync; read its diff and note the skaff version (the script prints it) in the PR body. If the baseline flags a kind `!!`, the regression is in skaff, not the spec.

The candidate run prints, per kind, whether the types and objects files change and how three signals move: generated structs, `apiextensionsv1.JSON` passthroughs, and CEL `XValidation` rules. A kind marked `!!` lost structs, gained a passthrough, lost validation, or stopped generating. Read `tmp/spec-preview/<kind>.diff` before going further — this is the operator's equivalent of a schema that used to generate and now gets skipped. See Step 5 for how to tell drift from breakage.

### 3. Start from a clean checkout, then branch

The pipeline rewrites files across `api`, `config`, `internal` and `cmd`, and the rollback in Step 5 restores those directories wholesale. Uncommitted work there would be mixed into the sync, or lost on rollback, so refuse to start if there is any:

```sh
test -z "$(git status --porcelain -- api config internal cmd tests)" || { echo "commit or stash your changes under api/ config/ internal/ cmd/ tests/ first"; exit 1; }
git switch -c chore/spec-sync-$(date +%Y%m%d)
```

### 4. Promote the candidate and run the full pipeline

```sh
cp tmp/spec/openapi.yml tmp/spec/openapi.prev.yml   # keep the old pin to restore
mv tmp/spec/openapi.next.yml tmp/spec/openapi.yml
bash .claude/skills/skaff-runner/scripts/run-pipeline.sh
```

Check the `>> spec` line it prints matches the hash the preview reported for the candidate, and the `>> skaff version` line matches the preview's. Do **not** edit `skaff.yml` either — if the spec moved an operation path or renamed a schema the config points at, the generators fail, and that is a resource change, not a sync.

### 5. Review the diff: catch-up or breakage

A wholesale regen legitimately pulls in drift you didn't ask for, in kinds nobody touched. That is the point — the operator tracks the current spec. What to expect:

- **catch-up** — fields added or removed on a kind's `<Kind>ObjectSpec`, new enum values (e.g. `Role`'s resource enum gaining a resource type), changed `+kubebuilder:validation` markers, updatable-field lists in `internal/resources/objects/` following the spec's update schemas, with `zz_generated.deepcopy.go` and `config/crd/bases/*.yaml` following the types.
- **breakage** — any kind the preview marked `!!`: typed sub-objects collapsing into one `apiextensionsv1.JSON` passthrough, a new `stays a passthrough` note on stderr for a field that used to be typed, CEL rules vanishing from a CRD, a `zz_generated_*` file disappearing, or `Rule`'s `UpdatableFields` regaining `evalWindow`/`frequency`/`preferredChannels`/`source`/`version` (the v1 revert from Step 1).
- **not a sync at all** — changes to a handwritten `<kind>_adapter.go`, a `cmd/main.go` registration block, `PROJECT`, or `go.mod`. A sync scaffolds no kinds, so none of these should move. Stop and work out why.

`grep -rn apiextensionsv1.JSON api/resources/v1alpha1/` lists every passthrough, including the ones skaff demotes silently.

**On breakage, stop — do not commit a partial or regressed regen.** Step 3 guaranteed these directories were clean, so restoring them discards only what this run generated. Restore the tree and the pin:

```sh
git restore -- api config internal cmd
mv tmp/spec/openapi.prev.yml tmp/spec/openapi.yml
```

Then work out which side owns the problem: a spec shape skaff cannot flatten is a skaff gap, and a wrong or missing schema is a signoz spec bug. Both live in other repos — hand the work to the session running there rather than editing them from here, and tell the user which kinds are blocked and why.

### 6. Hand-maintained inputs that follow the spec

The pipeline never rewrites a sample that exists. If a kind gained a required field or renamed one, update the matching `config/samples/resources_v1alpha1_<kind>.yaml`, and `tests/testdata/dashboard.json` for `Dashboard`, so they still validate against the new CRD.

### 7. Gates

Run the gates in the [go-checks](../../rules/go-checks.md) rule (`make checks`). The two `controllergen` runs must leave the tree clean — CI's `objects` and `manifests` jobs fail on any generated-file drift.

The e2e suite (`make test-e2e`; in CI only with the `safe-to-e2e` label) exercises `Dashboard` and the provider configs against the `signoz/signoz:latest` image, which can lag signoz main. A failure after a sync may be the server not having the new contract yet rather than the regen — check `GET /api/v1/version` on the running SigNoz before blaming the generated code.

### 8. Commit

One catch-up commit, regenerated code and any sample/testdata updates together:

```sh
git add -A api config internal tests
git commit -m "chore(resources): sync the generated kinds to the upstream openapi spec

Regenerated against SigNoz/signoz@$SPEC_SHA."
```

Name any upstream PRs merged into the spec in the body too.

### 9. Open the PR — confirm first

Opening a PR is outward-facing: **summarize the drift per kind and confirm with the user before `gh pr create`.** Record `SigNoz/signoz@<sha>`, any PRs merged into the spec, the skaff version the pipeline printed, and the notable per-kind drift in the body — the next sync starts from it. Follow the [pull-requests](../../rules/pull-requests.md) rule for the template and attribution; a sync is a **Chores** entry.

## Not this skill

- Adding a new mirrored kind → **resource-creator** (declares it in `skaff.yml`, scaffolds and registers it).
- Regenerating without changing the spec, e.g. after a `skaff.yml` edit → **skaff-runner** directly.