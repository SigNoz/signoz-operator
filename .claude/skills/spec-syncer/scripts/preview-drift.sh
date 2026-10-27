#!/usr/bin/env bash
#
# preview-drift.sh — show what regenerating against a spec would change, without
# touching the working tree.
#
# Renders the spec-derived generators (`skaff operator types` and `objects`)
# into a throwaway directory and compares each kind against the committed
# api/resources/v1alpha1/zz_generated_<kind>_types.go and
# internal/resources/objects/zz_generated_<kind>_object.go. For each kind it
# reports whether the file changed and how the regression signals moved:
#
#   structs       Go types in the kind's types file — a drop means typed
#                 sub-objects collapsed, often into one passthrough
#   passthroughs  fields carried as apiextensionsv1.JSON instead of a typed
#                 schema — a rise means a typed field was demoted
#   xvalidations  +kubebuilder:validation:XValidation rules — a drop means
#                 CEL checks the CRD used to enforce are gone
#
# A kind is marked !! when any signal moved the wrong way. That is a prompt to
# read its diff, not proof of breakage: upstream can legitimately remove a
# sub-object.
#
# adapters and controllers are not rendered: neither reads the spec.
#
# The rendered files and a unified diff per changed kind are kept under
# tmp/spec-preview/ (gitignored, replaced on every run), so a flagged kind's
# diff can be read without touching the checkout: tmp/spec-preview/<kind>.diff
#
# Usage:
#   preview-drift.sh [SPEC]   (default: tmp/spec/openapi.yml)
#
# Like the pipeline, it always renders with the latest skaff release. Run it
# against the current pinned spec first: a kind that does not report "same"
# there is re-rendered by the newer skaff, not by the new spec.
#
set -euo pipefail

WT="$(git rev-parse --show-toplevel)"
cd "$WT"
MOD="$(awk '/^module /{print $2; exit}' "$WT/go.mod")"

SPEC="${1:-$WT/tmp/spec/openapi.yml}"
if [ ! -f "$SPEC" ]; then
  echo "no spec at $SPEC" >&2
  exit 2
fi

PRIMUS_MK="${PRIMUS_HOME:-}/src/make/main.mk"
if [ -z "${PRIMUS_HOME:-}" ] || [ ! -f "$PRIMUS_MK" ]; then
  echo "PRIMUS_HOME is not set to a primus checkout — skaff comes from primus; set it up with the primus-setter skill" >&2
  exit 1
fi
# primus keeps a binary already at this path, so drop it to get the latest release.
SKAFF_BIN="$WT/tmp/bin/skaff"
rm -f "$SKAFF_BIN"
make -f "$PRIMUS_MK" "$SKAFF_BIN" >/dev/null

OUT="$WT/tmp/spec-preview"
rm -rf "$OUT"
mkdir -p "$OUT/types" "$OUT/objects"

echo ">> $("$SKAFF_BIN" --version 2>/dev/null || echo 'skaff (version unknown)') from tmp/bin"
echo ">> spec ${SPEC#"$WT"/}  $(wc -c < "$SPEC" | tr -d ' ') bytes  sha $(shasum "$SPEC" | cut -c1-12)"

"$SKAFF_BIN" operator types --config "$WT/skaff.yml" --openapi "$SPEC" --output "$OUT/types" 2> "$OUT/types.log" >/dev/null \
  || { cat "$OUT/types.log" >&2; echo "!! skaff operator types failed against this spec" >&2; exit 1; }
"$SKAFF_BIN" operator objects --config "$WT/skaff.yml" --openapi "$SPEC" --module "$MOD" --output "$OUT/objects" 2> "$OUT/objects.log" >/dev/null \
  || { cat "$OUT/objects.log" >&2; echo "!! skaff operator objects failed against this spec" >&2; exit 1; }

sort -u "$OUT/types.log" "$OUT/objects.log" | grep -v '\] wrote ' | sed 's/^/   skaff: /' || true

count() {
  if [ -f "$2" ]; then
    grep -c -- "$1" "$2" || true
  else
    echo 0
  fi
}

status() {
  local committed="$1" rendered="$2"
  if [ ! -f "$committed" ]; then
    echo "new"
  elif [ ! -f "$rendered" ]; then
    echo "MISSING"
  elif cmp -s "$committed" "$rendered"; then
    echo "same"
  else
    echo "changed($(diff "$committed" "$rendered" | grep -c '^[<>]'))"
  fi
}

FLAGGED=0
printf '\n%-20s %-13s %-13s %-10s %-13s %s\n' KIND TYPES OBJECTS STRUCTS PASSTHROUGHS XVALIDATIONS
for rendered in "$OUT"/types/zz_generated_*_types.go "$WT"/api/resources/v1alpha1/zz_generated_*_types.go; do
  [ -f "$rendered" ] || continue
  kind="$(basename "$rendered" _types.go)"
  kind="${kind#zz_generated_}"
  case " ${SEEN:-} " in *" $kind "*) continue ;; esac
  SEEN="${SEEN:-} $kind"

  ct="$WT/api/resources/v1alpha1/zz_generated_${kind}_types.go"
  nt="$OUT/types/zz_generated_${kind}_types.go"
  co="$WT/internal/resources/objects/zz_generated_${kind}_object.go"
  no="$OUT/objects/zz_generated_${kind}_object.go"

  sb="$(count '^type ' "$ct")"; sa="$(count '^type ' "$nt")"
  pb="$(count apiextensionsv1.JSON "$ct")"; pa="$(count apiextensionsv1.JSON "$nt")"
  xb="$(count XValidation "$ct")"; xa="$(count XValidation "$nt")"

  flag=""
  if [ "$sa" -lt "$sb" ] || [ "$pa" -gt "$pb" ] || [ "$xa" -lt "$xb" ] || [ ! -f "$nt" ]; then
    flag="  !!"
    FLAGGED=$((FLAGGED + 1))
  fi

  printf '%-20s %-13s %-13s %-10s %-13s %s%s\n' "$kind" "$(status "$ct" "$nt")" "$(status "$co" "$no")" "$sb -> $sa" "$pb -> $pa" "$xb -> $xa" "$flag"

  for pair in "$ct:$nt" "$co:$no"; do
    diff -u --label "${pair%%:*}" --label "${pair#*:}" "${pair%%:*}" "${pair#*:}" 2>/dev/null || true
  done | sed "s#$WT/##g" > "$OUT/$kind.diff"
  [ -s "$OUT/$kind.diff" ] || rm -f "$OUT/$kind.diff"
done

echo
if [ "$FLAGGED" -eq 0 ]; then
  echo ">> no kind lost structs, typed fields or CEL validation"
else
  echo ">> $FLAGGED kind(s) marked !! lost structs, typed fields or CEL validation, or stopped generating — read their diff before regenerating"
fi
echo ">> rendered files and per-kind diffs in tmp/spec-preview/"
