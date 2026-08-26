#!/bin/bash
# Ships a signoz-operator release in the helm chart, from a clean checkout of
# SigNoz/charts through to an open pull request.
#
# The chart itself lives in SigNoz/charts and is hand written there. Only the
# CRDs, the manager role rules and the chart version are derived from this
# repository, and only those are rewritten here.
#
# Run from the root of this repository, checked out at the tag being released.
#
# Usage: hack/chart.sh <version>
#
#   version  release tag, e.g. v0.1.0. The chart version and appVersion are kept
#            in lockstep with it: appVersion is the tag, version is the tag
#            without the leading v.
#
# Environment:
#   GITHUB_TOKEN  token allowed to push to CHARTS_REPO and open pull requests on
#                 it. Read from the environment rather than an argument so that
#                 it stays out of the process list.
#   GIT_USER      name recorded as the author of the chart commit
#   GIT_EMAIL     email recorded as the author of the chart commit
#   CHARTS_REPO   chart repository, defaults to SigNoz/charts
#   CHARTS_BASE   branch the pull request targets, defaults to main
#
# Doing nothing is a success: the script exits 0 when the release branch is
# already on the remote, or when the sync leaves the chart unchanged.

set -euo pipefail

readonly VERSION="${1:?usage: hack/chart.sh <version>}"
readonly TOKEN="${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
readonly AUTHOR="${GIT_USER:?GIT_USER is required}"
readonly EMAIL="${GIT_EMAIL:?GIT_EMAIL is required}"
readonly REPO="${CHARTS_REPO:-SigNoz/charts}"
readonly BASE="${CHARTS_BASE:-main}"

readonly CRD_DIR="config/crd/bases"
readonly CLUSTER_ROLE="config/rbac/clusterrole.generated.yaml"
readonly LEADER_ROLE="config/rbac/role.yaml"
readonly CHART="charts/signoz-operator"

##############################################################################
# Emits the rules of a role source file, stripping everything above "rules:".
# Arguments:
#   path to the role file
##############################################################################
rules() {
  awk '/^rules:/ { emit = 1 } emit { print }' "$1"
}

##############################################################################
# Emits a CRD as a chart template: guarded by crds.install, with the chart's
# annotations appended to the ones controller-gen already writes and a labels
# block added next to the name.
# Arguments:
#   path to the controller-gen CRD
##############################################################################
crd() {
  echo '{{- if .Values.crds.install }}'
  awk '
    /^    controller-gen\.kubebuilder\.io\/version:/ {
      print
      print "    {{- include \"signoz-operator.crdAnnotations\" . | nindent 4 }}"
      next
    }
    /^  name: / && !labelled {
      print
      print "  labels:"
      print "    {{- include \"signoz-operator.crdLabels\" . | nindent 4 }}"
      labelled = 1
      next
    }
    { print }
  ' "$1"
  echo '{{- end }}'
}

if [[ ! "${VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "invalid version: ${VERSION}" >&2
  exit 1
fi

WORK="$(mktemp -d)"
readonly WORK
trap 'rm -rf "${WORK}"' EXIT

readonly BRANCH="release/signoz-operator-${VERSION#v}"

echo ">> Cloning ${REPO}"
# Anonymously, so that the token never reaches the fetch URL. It is needed for
# the push and for the pull request only.
git clone --quiet --depth 1 --branch "${BASE}" "https://github.com/${REPO}.git" "${WORK}"
git -C "${WORK}" remote set-url --push origin "https://x-access-token:${TOKEN}@github.com/${REPO}.git"

if [[ ! -d "${WORK}/${CHART}/templates/crds" ]]; then
  echo "not a signoz-operator chart checkout: ${REPO}@${BASE}" >&2
  exit 1
fi

if git -C "${WORK}" ls-remote --exit-code --heads origin "${BRANCH}" >/dev/null; then
  echo ">> Branch ${BRANCH} already exists, nothing to do"
  exit 0
fi

echo ">> Syncing CRDs"
# Wholesale, so that a kind removed from the operator leaves the chart too. Safe
# as a glob because templates/crds/ holds nothing hand written.
rm -f "${WORK}/${CHART}"/templates/crds/*.yaml
for source in "${CRD_DIR}"/*.yaml; do
  name="$(basename "${source}" .yaml)"
  crd "${source}" > "${WORK}/${CHART}/templates/crds/${name#resources.signoz.io_}.yaml"
done

echo ">> Syncing roles"
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
  cat <<'EOF'
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ include "signoz-operator.resourceName" (dict "suffix" "leader-election" "context" .) }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "signoz-operator.labels" . | nindent 4 }}
EOF
  rules "${LEADER_ROLE}"
} > "${WORK}/${CHART}/templates/role.yaml"

echo ">> Setting chart version to ${VERSION#v}"
yq -i ".version = \"${VERSION#v}\" | .appVersion = \"${VERSION}\"" "${WORK}/${CHART}/Chart.yaml"

git -C "${WORK}" config user.name "${AUTHOR}"
git -C "${WORK}" config user.email "${EMAIL}"
git -C "${WORK}" checkout --quiet -b "${BRANCH}"
git -C "${WORK}" add "${CHART}"
if git -C "${WORK}" diff --cached --quiet; then
  echo ">> Chart is already up to date, nothing to do"
  exit 0
fi

echo ">> Pushing ${BRANCH}"
git -C "${WORK}" commit --quiet -m "chore(signoz-operator): bump SigNoz Operator to ${VERSION}"
git -C "${WORK}" push --quiet origin "${BRANCH}"

echo ">> Opening the pull request"
GH_TOKEN="${TOKEN}" gh pr create --repo "${REPO}" --base "${BASE}" --head "${BRANCH}" \
  --title "chore(signoz-operator): bump SigNoz Operator to ${VERSION}" \
  --body "#### Chores

- Sync the CRDs and the manager role from SigNoz Operator ${VERSION}.
- Bump the \`signoz-operator\` chart to ${VERSION#v}."
