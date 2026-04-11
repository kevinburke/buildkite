#!/usr/bin/env bash
set -euo pipefail

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  echo "source .buildkite/ci/setup-env.sh from another script" >&2
  exit 1
fi

if [[ -n "${BUILDKITE_CI_ENV_READY:-}" ]]; then
  return 0
fi
export BUILDKITE_CI_ENV_READY=1

SETUP_ENV_SCRIPT_DIR="$(
  CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
)"
readonly SETUP_ENV_SCRIPT_DIR
SETUP_ENV_REPO_ROOT="$(
  CDPATH='' cd -- "${SETUP_ENV_SCRIPT_DIR}/../.." && pwd
)"
readonly SETUP_ENV_REPO_ROOT

cd "${SETUP_ENV_REPO_ROOT}"

export GOTOOLCHAIN=local
export DIFFER_VERSION="${DIFFER_VERSION:-v0.0.0-20260403230520-c0574ebcacb2}"
export STATICCHECK_VERSION="${STATICCHECK_VERSION:-v0.7.0}"

goflags="${GOFLAGS:-}"
if [[ -n "${goflags}" ]]; then
  goflags+=" "
fi
goflags+="-mod=vendor -trimpath"
export GOFLAGS="${goflags}"

export TOOL_CACHE_ROOT="${TOOL_CACHE_ROOT:-$(dirname -- "${GOCACHE:-${SETUP_ENV_REPO_ROOT}/.cache/go-build}")/tools}"
export GOBIN="${TOOL_CACHE_ROOT}/bin"
readonly TOOL_STAMP_DIR="${TOOL_CACHE_ROOT}/stamps"

mkdir -p "${GOBIN}" "${TOOL_STAMP_DIR}"
export PATH="${GOBIN}:${PATH}"

GO_VERSION="$(
  go env GOVERSION
)"
echo "Using ${GO_VERSION}"

install_tool() {
  local binary="$1"
  local pkg="$2"
  local version="$3"
  local stamp_file="${TOOL_STAMP_DIR}/${binary}-${version}"

  if [[ -x "${GOBIN}/${binary}" && -f "${stamp_file}" ]]; then
    return
  fi

  find "${TOOL_STAMP_DIR}" -maxdepth 1 -type f -name "${binary}-*" -delete
  GOFLAGS="" GOBIN="${GOBIN}" go install "${pkg}@${version}"
  : > "${stamp_file}"
}
