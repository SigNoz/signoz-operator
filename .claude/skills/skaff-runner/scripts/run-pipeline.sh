#!/usr/bin/env bash
#
# run-pipeline.sh — run the ENTIRE skaff operator codegen pipeline deterministically.
#
# Regenerates every kind declared in skaff.yml from the pinned spec at
# tmp/spec/openapi.yml, in order: types → objects → adapters (scaffold,
# write-if-missing) → controllers (which also drives kubebuilder: `create api`
# for kinds missing from PROJECT, and the cmd/main.go registration blocks),
# then controller-gen so deepcopy and the CRDs match the regenerated types.
# Scaffolding a NEW kind needs `kubebuilder` on PATH.
#
# adapters runs before controllers because the cmd/main.go block controllers
# writes calls adapters.New<Kind>Adapter() — generated the other way round, the
# tree does not compile until the adapter shows up.
#
# Both tools come from primus and nowhere else: it downloads the skaff release
# into tmp/bin and pins controller-gen to the version CI runs, so the generator
# rendering this repo is the same one every other machine gets. PRIMUS_HOME
# must be set — the primus-setter skill sets it up.
#
# The spec is a pinned input read from tmp/spec/openapi.yml. Preparing that
# file is a deliberate step outside this script — from a signoz branch, from a
# constructed merge, or from origin/main by default — so that everything
# generated below is traceable to one file you can inspect.
#
# It does NOT edit skaff.yml, prepare the spec, hand-complete adapter Find
# logic, flesh out samples, or run the build/lint/test gate. It reports the
# adapter and sample stubs it leaves behind.
#
# Usage:
#   run-pipeline.sh          (no arguments; reads tmp/spec/openapi.yml)
#
# Env:
#   SKAFF_VERSION=vX.Y.Z     skaff release for primus to fetch (default: latest)
#   SKAFF_REFRESH=1          drop tmp/bin/skaff first, forcing a re-download
#   DRY_RUN=1                print each invocation instead of running it
#   SKIP_CONTROLLERGEN=1     skip the trailing primus controllergen runs
#
set -euo pipefail

if [ "$#" -gt 0 ]; then
  echo "usage: $0   (no arguments — the spec is read from tmp/spec/openapi.yml; put your spec there)" >&2
  exit 2
fi

WT="$(git rev-parse --show-toplevel)"
cd "$WT"
MOD="$(awk '/^module /{print $2; exit}' "$WT/go.mod")"

# primus provisions both tools, so a missing one is worth catching now rather
# than four generators into a rewritten tree.
PRIMUS_MK="${PRIMUS_HOME:-}/src/make/main.mk"
if [ -z "${PRIMUS_HOME:-}" ] || [ ! -f "$PRIMUS_MK" ]; then
  echo "PRIMUS_HOME is not set to a primus checkout — skaff and controller-gen both come from primus, which is what pins them to the versions CI uses" >&2
  echo "  set it up with the primus-setter skill" >&2
  exit 1
fi

SPEC="$WT/tmp/spec/openapi.yml"
if [ ! -f "$SPEC" ]; then
  cat >&2 <<'MSG'
no spec at tmp/spec/openapi.yml — prepare one there first. Unless you were told
which branch or construction to use, the default is signoz's current main:

  mkdir -p tmp/spec
  git -C ../signoz fetch --quiet origin
  git -C ../signoz show origin/main:docs/api/openapi.yml > tmp/spec/openapi.yml
MSG
  exit 2
fi

run() {
  echo ">> $*"
  if [ -z "${DRY_RUN:-}" ]; then
    "$@"
  fi
}

# primus downloads the release into its BIN_DIR, which is ./tmp/bin, and every
# generator below runs that binary. primus keeps a binary that is already
# there, so SKAFF_REFRESH=1 is how you move to a newer release — and how you
# clear out anything that landed at this path by other means.
SKAFF_BIN="$WT/tmp/bin/skaff"
[ -z "${SKAFF_REFRESH:-}" ] || rm -f "$SKAFF_BIN"
run make -f "$PRIMUS_MK" "$SKAFF_BIN"

echo ">> $("$SKAFF_BIN" --version 2>/dev/null || echo 'skaff (version unknown)') from tmp/bin, via primus"
echo ">> spec tmp/spec/openapi.yml  $(wc -c < "$SPEC" | tr -d ' ') bytes  sha $(shasum "$SPEC" | cut -c1-12)"
echo ">> skaff operator pipeline  repo=$WT  module=$MOD"

run "$SKAFF_BIN" operator types --config "$WT/skaff.yml" --openapi "$SPEC" --output "$WT/api/resources/v1alpha1"
run "$SKAFF_BIN" operator objects --config "$WT/skaff.yml" --openapi "$SPEC" --module "$MOD" --output "$WT/internal/resources/objects"
run "$SKAFF_BIN" operator adapters --config "$WT/skaff.yml" --module "$MOD" --output "$WT/internal/resources/adapters"
run "$SKAFF_BIN" operator controllers --config "$WT/skaff.yml" --module "$MOD" --output "$WT/internal/controller/resources"

if [ -z "${SKIP_CONTROLLERGEN:-}" ]; then
  run make -f "$PRIMUS_MK" controllergen CONTROLLERGEN_ARGS='object paths="./..."'
  run make -f "$PRIMUS_MK" controllergen CONTROLLERGEN_ARGS='rbac:roleName=manager-role crd:allowDangerousTypes=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases'
fi

if [ -n "${DRY_RUN:-}" ]; then
  echo ">> (dry run) nothing was generated"
  exit 0
fi

# The two things the pipeline deliberately leaves to a human are both findable,
# so list them instead of asking the caller to remember to go looking. A stub
# Find compiles and reconciles — it just answers "never found" forever — so it
# fails silently at runtime rather than at the build gate.
TODO=0

for adapter in "$WT"/internal/resources/adapters/*_adapter.go; do
  [ -f "$adapter" ] || continue
  if grep -A1 ') Find(' "$adapter" | grep -q '^	return nil, nil$'; then
    echo "!! stub Find — the kind is registered and will never find its remote: ${adapter#"$WT"/}"
    TODO=$((TODO + 1))
  fi
done

for sample in "$WT"/config/samples/*.yaml; do
  [ -f "$sample" ] || continue
  if grep -q 'TODO(user)' "$sample"; then
    echo "!! sample is still kubebuilder's stub: ${sample#"$WT"/}"
    TODO=$((TODO + 1))
  fi
done

if [ "$TODO" -eq 0 ]; then
  echo ">> done — nothing left by hand; run the build/lint/test gate"
else
  echo ">> done — $TODO file(s) above need hand-completion, then the build/lint/test gate"
fi
