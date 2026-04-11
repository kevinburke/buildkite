#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(
  CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
)"
readonly SCRIPT_DIR

# shellcheck source=.buildkite/ci/setup-env.sh
source "${SCRIPT_DIR}/setup-env.sh"

install_tool staticcheck honnef.co/go/tools/cmd/staticcheck "${STATICCHECK_VERSION:?STATICCHECK_VERSION is required}"

mkdir -p reports/lint

go vet ./... 2>&1 | tee reports/lint/go-vet.txt
staticcheck ./... 2>&1 | tee reports/lint/staticcheck.txt
