#!/bin/bash
# Syncs the generated parts of the signoz-operator helm chart into a checkout
# of SigNoz/charts. The chart itself lives in SigNoz/charts and is hand
# written there; only the CRDs and the manager role rules are derived from
# this repository, and only those are rewritten here.
#
# Run from the root of this repository, checked out at the tag being released.
#
# Usage: hack/chart.sh <chart-dir> [version]
#
#   chart-dir  path to charts/signoz-operator in a SigNoz/charts checkout
#   version    release tag, e.g. v0.1.0. When given, the chart version and
#              appVersion are set from it and kept in lockstep: appVersion is
#              the tag, version is the tag without the leading v.

set -euo pipefail

readonly CHART_DIR="${1:?usage: hack/chart.sh <chart-dir> [version]}"
readonly VERSION="${2:-}"

readonly CRD_DIR="config/crd/bases"
readonly CLUSTER_ROLE="config/rbac/clusterrole.generated.yaml"

##############################################################################
# Emits the rules of a role source file, stripping everything above "rules:".
# Arguments:
#   path to the role file
##############################################################################
rules() {
  awk '/^rules:/ { emit = 1 } emit { print }' "$1"
}

if [[ ! -d "${CHART_DIR}/crds" ]]; then
  echo "not a signoz-operator chart checkout: ${CHART_DIR}" >&2
  exit 1
fi

echo ">> Syncing CRDs"
# Wholesale, so that a kind removed from the operator leaves the chart too.
# Safe as a glob because crds/ holds nothing hand written. Helm does not
# render crds/ as templates, so these ship exactly as controller-gen wrote
# them, with no guard or annotation wrapped around them.
rm -f "${CHART_DIR}"/crds/*.yaml
for source in "${CRD_DIR}"/*.yaml; do
  name="$(basename "${source}" .yaml)"
  cp "${source}" "${CHART_DIR}/crds/${name#resources.signoz.io_}.yaml"
done

echo ">> Syncing manager role"
{
  cat <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
{{- if .Values.rbac.namespaced }}
kind: Role
{{- else }}
kind: ClusterRole
{{- end }}
metadata:
  name: {{ include "signoz-operator.fullname" . }}
  {{- if .Values.rbac.namespaced }}
  namespace: {{ .Release.Namespace }}
  {{- end }}
  labels:
    {{- include "signoz-operator.labels" . | nindent 4 }}
EOF
  rules "${CLUSTER_ROLE}"
} > "${CHART_DIR}/templates/manager-role.yaml"

if [[ -n "${VERSION}" ]]; then
  if [[ ! "${VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "invalid version: ${VERSION}" >&2
    exit 1
  fi
  echo ">> Setting chart version to ${VERSION#v}"
  yq -i ".version = \"${VERSION#v}\" | .appVersion = \"${VERSION}\"" "${CHART_DIR}/Chart.yaml"
fi

echo ">> Synced ${CHART_DIR}"
