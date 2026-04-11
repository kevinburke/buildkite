#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(
  CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
)"
readonly SCRIPT_DIR

# shellcheck source=.buildkite/ci/setup-env.sh
source "${SCRIPT_DIR}/setup-env.sh"

install_tool differ github.com/kevinburke/differ "${DIFFER_VERSION:?DIFFER_VERSION is required}"

mkdir -p reports/format

differ go fmt ./... 2>&1 | tee reports/format/go-fmt.txt
